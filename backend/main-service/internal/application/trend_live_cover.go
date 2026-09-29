package application

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	liveCoverOrigin     = "https://arxiv.org"
	liveCoverCandidates = 5
	liveCoverTimeout    = 8 * time.Second
	liveCoverRequest    = 4 * time.Second
	liveCoverMaxPage    = 16 << 20
	liveCoverMaxImage   = 3 << 20
	liveCoverEntries    = 160
	liveCoverBytes      = 64 << 20
	liveCoverTTL        = 24 * time.Hour
	liveCoverMissTTL    = time.Hour
)

var (
	liveCoverArxivID = regexp.MustCompile(`^\d{4}\.\d{4,5}$`)
	// Путь рисунка после «/html/<id>»: ни один сегмент не начинается с точки, поэтому «..» не пролезает даже в
	// процентной записи; расширение — только растр.
	liveCoverImagePath = regexp.MustCompile(`(?i)^(?:v\d+)?(?:/[\w-][\w.-]*)+\.(?:png|jpe?g|gif|webp)$`)
)

// LiveCover — «Рисунок 1» статьи: в ML это почти всегда схема метода. Показывается только вместе со статьёй,
// иначе чужой рисунок читался бы как наш.
type LiveCover struct {
	ImageURL string         `json:"image_url"`
	Caption  string         `json:"caption"`
	Figure   string         `json:"figure,omitempty"`
	Paper    LiveCoverPaper `json:"paper"`
	Image    []byte         `json:"-"`
	MIME     string         `json:"-"`
}

type LiveCoverPaper struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type liveCoverEntry struct {
	cover   LiveCover
	expires time.Time
}

// LiveCovers кэширует и пустой ответ: без кэша каждое открытие карточки стоило бы до десяти обращений к arXiv.
// ponytail: кэш в памяти одной реплики и без общей записи «уже ищу» — два одновременных первых захода
// сходят в arXiv дважды; вынести в Valkey, когда реплик станет больше одной.
type LiveCovers struct {
	live  *TrendLiveService
	http  *http.Client
	mu    sync.Mutex
	items map[string]liveCoverEntry
	order []string
	size  int
}

func NewLiveCovers(live *TrendLiveService) *LiveCovers {
	return &LiveCovers{live: live, items: map[string]liveCoverEntry{},
		http: &http.Client{Timeout: liveCoverRequest, CheckRedirect: liveCoverRedirect}}
}

// Адрес строится из проверенного пути, но перенаправление подменило бы хост уже после проверки.
func liveCoverRedirect(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme+"://"+req.URL.Host != liveCoverOrigin || len(via) > 3 {
		return http.ErrUseLastResponse
	}
	return nil
}

// Cover без ошибки и с нулевой обложкой — рисунка нет; ErrNotFound — такого направления в снимке нет.
func (c *LiveCovers) Cover(ctx context.Context, id string) (LiveCover, error) {
	return c.cached(ctx, id, func() ([]StudyPaper, bool) { return c.live.coverPapers(id) })
}

// PaperCover собирает адрес из проверенного номера arXiv; иной номер — ErrNotFound.
func (c *LiveCovers) PaperCover(ctx context.Context, id string) (LiveCover, error) {
	arxiv := liveArxivVersion.ReplaceAllString(strings.TrimPrefix(id, "arxiv:"), "")
	if !liveCoverArxivID.MatchString(arxiv) {
		return LiveCover{}, ErrNotFound
	}
	return c.cached(ctx, "paper:"+arxiv, func() ([]StudyPaper, bool) {
		return []StudyPaper{{ID: "arxiv:" + arxiv}}, true
	})
}

func (c *LiveCovers) cached(ctx context.Context, key string, papers func() ([]StudyPaper, bool)) (LiveCover, error) {
	c.mu.Lock()
	entry, ok := c.items[key]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.cover, nil
	}
	list, ok := papers()
	if !ok {
		return LiveCover{}, ErrNotFound
	}
	// Уход со страницы не выбрасывает почти найденный рисунок: свой срок работу всё равно ограничит.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), liveCoverTimeout)
	defer cancel()
	cover := c.resolve(ctx, list)
	switch {
	case cover.Image != nil:
		c.store(key, cover, liveCoverTTL)
	case ctx.Err() == nil:
		// Истёкший срок — медленный arXiv, а не отсутствие рисунка: иначе обложка спряталась бы на час.
		c.store(key, cover, liveCoverMissTTL)
	}
	return cover, nil
}

func (c *LiveCovers) resolve(ctx context.Context, papers []StudyPaper) LiveCover {
	for _, paper := range papers {
		cover, err := c.fromPaper(ctx, paper)
		if err == nil {
			return cover
		}
		slog.Debug("live cover skipped", "paper", paper.ID, "error", err)
		if ctx.Err() != nil {
			break
		}
	}
	return LiveCover{}
}

func (c *LiveCovers) fromPaper(ctx context.Context, paper StudyPaper) (LiveCover, error) {
	id := liveArxivVersion.ReplaceAllString(strings.TrimPrefix(paper.ID, "arxiv:"), "")
	if !liveCoverArxivID.MatchString(id) {
		return LiveCover{}, fmt.Errorf("live cover: %q is not an arXiv id", paper.ID)
	}
	page, err := c.get(ctx, liveCoverOrigin+"/html/"+id, liveCoverMaxPage)
	if err != nil {
		return LiveCover{}, err
	}
	src, number, caption, ok := liveCoverFigure(page, id)
	if !ok {
		return LiveCover{}, fmt.Errorf("live cover: %s has no raster figure", id)
	}
	image, err := c.get(ctx, src, liveCoverMaxImage)
	if err != nil {
		return LiveCover{}, err
	}
	// Тип — по байтам, а не по заголовку: отдаём от своего имени, и под видом картинки не уйдёт ничего другого.
	mime := http.DetectContentType(image)
	if !slices.Contains([]string{"image/png", "image/jpeg", "image/gif", "image/webp"}, mime) {
		return LiveCover{}, fmt.Errorf("live cover: %s is %s", src, mime)
	}
	return LiveCover{Caption: caption, Figure: number, Image: image, MIME: mime,
		Paper: LiveCoverPaper{ID: paper.ID, Title: paper.Title, URL: liveCoverOrigin + "/abs/" + id}}, nil
}

func (c *LiveCovers) get(ctx context.Context, endpoint string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("live cover: build request: %w", err)
	}
	req.Header.Set("User-Agent", "rag-test-signalgraph/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("live cover: get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("live cover: %s answered %d", endpoint, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("live cover: read body: %w", err)
	}
	// Обрезанная картинка битая, а не маленькая: сверх предела — отказ.
	if len(body) > limit {
		return nil, fmt.Errorf("live cover: %s is larger than %d bytes", endpoint, limit)
	}
	return body, nil
}

// Предел и по числу, и по байтам: рисунок весит от десятков килобайт до трёх мегабайт.
func (c *LiveCovers) store(id string, cover LiveCover, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.items[id]; ok {
		c.size -= len(old.cover.Image)
		c.order = slices.DeleteFunc(c.order, func(key string) bool { return key == id })
	}
	c.items[id] = liveCoverEntry{cover: cover, expires: time.Now().Add(ttl)}
	c.order = append(c.order, id)
	c.size += len(cover.Image)
	for len(c.order) > liveCoverEntries || c.size > liveCoverBytes {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.size -= len(c.items[oldest].cover.Image)
		delete(c.items, oldest)
	}
}

// Перебор ограничен: у части статей нет HTML-версии или растровых рисунков.
func (s *TrendLiveService) coverPapers(id string) ([]StudyPaper, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot == nil {
		return nil, false
	}
	for _, topics := range [][]LiveTopic{s.snapshot.Topics, s.snapshot.Discovered} {
		if i := slices.IndexFunc(topics, func(topic LiveTopic) bool { return topic.ID == id }); i >= 0 {
			return liveCoverOrder(topics[i], s.snapshot.Papers, s.snapshot.PaperMetadata), true
		}
	}
	return nil, false
}

// Схема частного применения из кейса карточки вводит в заблуждение, поэтому статьи о самом направлении идут первыми.
func liveCoverOrder(topic LiveTopic, papers []StudyPaper, metadata map[string]LivePaperMetadata) []StudyPaper {
	var chosen []string
	if topic.Card != nil {
		chosen = append(chosen, topic.Card.ExampleID)
		for _, claim := range topic.Card.Evidence {
			chosen = append(chosen, claim.SourceID)
		}
	}
	rank := func(paper StudyPaper) int {
		if i := slices.Index(chosen, paper.ID); i >= 0 {
			return i
		}
		return len(chosen)
	}
	names := slices.DeleteFunc(append([]string{topic.Name, topic.Label}, topic.Query.Terms...), func(term string) bool {
		return len([]rune(strings.TrimSpace(term))) < 3
	})
	contains := func(title string, words []string) bool {
		title = strings.ToLower(title)
		return slices.ContainsFunc(words, func(word string) bool {
			return strings.Contains(title, strings.ToLower(strings.TrimSpace(word)))
		})
	}
	// Слова заголовка обзоров и работ, вводящих подход: у них первый рисунок — общая схема.
	overview := []string{"survey", "overview", "tutorial", "framework", "benchmark", "taxonomy"}
	missing := func(ok bool) int {
		if ok {
			return 0
		}
		return 1
	}
	cited := func(paper StudyPaper) int {
		if count := metadata[paper.ID].CitedBy; count != nil {
			return *count
		}
		return 0
	}
	own := make(map[string]bool, len(topic.PaperIDs))
	for _, id := range topic.PaperIDs {
		own[id] = true
	}
	out := []StudyPaper{}
	for _, paper := range papers {
		if own[paper.ID] && strings.HasPrefix(paper.ID, "arxiv:") {
			out = append(out, paper)
		}
	}
	slices.SortStableFunc(out, func(a, b StudyPaper) int {
		return cmp.Or(
			cmp.Compare(missing(contains(a.Title, names)), missing(contains(b.Title, names))),
			cmp.Compare(missing(contains(a.Title, overview)), missing(contains(b.Title, overview))),
			cmp.Compare(cited(b), cited(a)),
			cmp.Compare(rank(a), rank(b)),
			strings.Compare(b.Date, a.Date),
		)
	})
	return out[:min(len(out), liveCoverCandidates)]
}

func liveCoverFigure(page []byte, id string) (src, number, caption string, ok bool) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", "", "", false
	}
	for figure := range doc.Descendants() {
		if figure.DataAtom != atom.Figure || !liveHTMLClass(figure, "ltx_figure") {
			continue
		}
		for img := range figure.Descendants() {
			if img.DataAtom != atom.Img {
				continue
			}
			if src, ok = liveCoverImageURL(id, liveHTMLAttr(img, "src")); ok {
				number, caption = liveCoverCaption(figure)
				return src, number, caption, true
			}
		}
	}
	return "", "", "", false
}

// liveCoverImageURL принимает только относительный путь внутри HTML-версии той же статьи: абсолютный адрес
// из внешней разметки превратил бы прокси в запрос куда угодно.
func liveCoverImageURL(id, src string) (string, bool) {
	ref, err := url.Parse(src)
	if err != nil || ref.Scheme != "" || ref.Host != "" || ref.User != nil || strings.HasPrefix(ref.Path, "/") {
		return "", false
	}
	path := (&url.URL{Path: "/html/" + id}).ResolveReference(ref).Path
	rest, ok := strings.CutPrefix(path, "/html/"+id)
	if !ok || !liveCoverImagePath.MatchString(rest) {
		return "", false
	}
	return liveCoverOrigin + path, true
}

// liveCoverCaption пропускает скрытые описания для экранных читалок, иначе подпись повторялась бы дважды.
func liveCoverCaption(figure *html.Node) (number, caption string) {
	var text strings.Builder
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		switch {
		case liveHTMLClass(n, "ltx_nodisplay"):
			return
		case liveHTMLClass(n, "ltx_tag"):
			var tag strings.Builder
			for d := range n.Descendants() {
				if d.Type == html.TextNode {
					tag.WriteString(d.Data)
				}
			}
			if fields := strings.Fields(tag.String()); number == "" && len(fields) > 0 {
				number = strings.TrimRight(fields[len(fields)-1], ".:")
			}
			return
		case n.DataAtom == atom.Math:
			text.WriteString(liveHTMLAttr(n, "alttext"))
			return
		case n.Type == html.TextNode:
			text.WriteString(n.Data)
		}
		for child := range n.ChildNodes() {
			visit(child)
		}
	}
	for child := range figure.ChildNodes() {
		if child.DataAtom == atom.Figcaption {
			visit(child)
			break
		}
	}
	return number, strings.Join(strings.Fields(text.String()), " ")
}

func liveHTMLAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func liveHTMLClass(n *html.Node, class string) bool {
	return n.Type == html.ElementNode && slices.Contains(strings.Fields(liveHTMLAttr(n, "class")), class)
}
