package application

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type liveQuery func(title, text []string) bool

// liveCompileQuery проверяет запрос arXiv локально по уже перечисленному окну OAI: тот же корпус, что
// знаменатель, и ни одного 429.
func liveCompileQuery(query string) (liveQuery, error) {
	compiled, _, err := liveCompileQueryPhrases(query)
	return compiled, err
}

// liveCompileQueryPhrases отдаёт и фразы: отрицания в синтаксисе нет, поэтому отбор кандидатов по индексу окна
// не теряет совпадений.
func liveCompileQueryPhrases(query string) (liveQuery, [][]string, error) {
	p := &liveQueryParser{rest: query}
	compiled, err := p.or()
	if err != nil {
		return nil, nil, err
	}
	if rest := strings.TrimSpace(p.rest); rest != "" {
		return nil, nil, fmt.Errorf("live query: unexpected %q", rest)
	}
	return compiled, p.phrases, nil
}

type liveQueryParser struct {
	rest    string
	phrases [][]string
}

func (p *liveQueryParser) or() (liveQuery, error) {
	left, err := p.and()
	for err == nil && p.keyword("OR") {
		var right liveQuery
		if right, err = p.and(); err == nil {
			left = liveEither(left, right)
		}
	}
	return left, err
}

func (p *liveQueryParser) and() (liveQuery, error) {
	left, err := p.term()
	for err == nil && p.keyword("AND") {
		var right liveQuery
		if right, err = p.term(); err == nil {
			left = liveBoth(left, right)
		}
	}
	return left, err
}

func (p *liveQueryParser) keyword(word string) bool {
	rest, ok := strings.CutPrefix(strings.TrimLeft(p.rest, " "), word+" ")
	if ok {
		p.rest = rest
	}
	return ok
}

func (p *liveQueryParser) term() (liveQuery, error) {
	p.rest = strings.TrimLeft(p.rest, " ")
	if rest, ok := strings.CutPrefix(p.rest, "("); ok {
		p.rest = rest
		inner, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.rest, ok = strings.CutPrefix(strings.TrimLeft(p.rest, " "), ")"); !ok {
			return nil, errors.New("live query: missing )")
		}
		return inner, nil
	}
	field, rest, ok := strings.Cut(p.rest, ":")
	if !ok || field == "" || strings.ContainsAny(field, ` ()"`) {
		return nil, fmt.Errorf("live query: expected field:term at %q", p.rest)
	}
	var raw string
	if quoted, isPhrase := strings.CutPrefix(rest, `"`); isPhrase {
		if raw, rest, ok = strings.Cut(quoted, `"`); !ok {
			return nil, errors.New("live query: unterminated phrase")
		}
	} else {
		end := strings.IndexAny(rest, " )")
		if end < 0 {
			end = len(rest)
		}
		raw, rest = rest[:end], rest[end:]
	}
	p.rest = rest
	phrase := liveStems(liveTokens(raw))
	if len(phrase) == 0 {
		return nil, fmt.Errorf("live query: empty term %q", raw)
	}
	p.phrases = append(p.phrases, phrase)
	if field == "ti" {
		return func(title, _ []string) bool { return livePhraseIn(title, phrase) }, nil
	}
	return func(_, text []string) bool { return livePhraseIn(text, phrase) }, nil
}

func liveEither(a, b liveQuery) liveQuery {
	return func(title, text []string) bool { return a(title, text) || b(title, text) }
}

func liveBoth(a, b liveQuery) liveQuery {
	return func(title, text []string) bool { return a(title, text) && b(title, text) }
}

func livePhraseIn(tokens, phrase []string) bool {
	if len(phrase) == 1 {
		return slices.Contains(tokens, phrase[0])
	}
	for i := 0; i+len(phrase) <= len(tokens); i++ {
		if slices.Equal(tokens[i:i+len(phrase)], phrase) {
			return true
		}
	}
	return false
}

func liveStems(tokens []string) []string {
	for i, token := range tokens {
		tokens[i] = liveStem(token)
	}
	return tokens
}

func liveMatchFamilies(families []liveFamily, window []StudyPaper, metadata map[string]LivePaperMetadata,
) ([][]StudyPaper, error) {
	queries := make([]liveQuery, len(families))
	for i, family := range families {
		query, err := liveCompileQuery(family.query.Arxiv)
		if err != nil {
			return nil, fmt.Errorf("live: family %s: %w", family.id, err)
		}
		queries[i] = query
	}
	members := make([][]StudyPaper, len(families))
	for _, paper := range window {
		title := liveStems(liveTokens(paper.Title))
		text := liveStems(liveTokens(paper.Title + " " + metadata[paper.ID].Abstract))
		for i, query := range queries {
			if query(title, text) {
				members[i] = append(members[i], paper)
			}
		}
	}
	for _, papers := range members {
		slices.SortFunc(papers, func(a, b StudyPaper) int {
			return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.ID, b.ID))
		})
	}
	return members, nil
}

// livePriorRepos — счёт репозиториев по закрытым месяцам из прошлого снимка: прошлое у GitHub не
// меняется, а без токена каждый месяц стоит отдельного медленного запроса.
func livePriorRepos(snapshot *LiveSnapshot) map[string]map[string]int {
	prior := map[string]map[string]int{}
	if snapshot == nil {
		return prior
	}
	for _, topic := range snapshot.Topics {
		if len(topic.Repos) != len(snapshot.Periods) {
			continue
		}
		counts := map[string]int{}
		for i, period := range snapshot.Periods {
			if period.Complete {
				counts[period.Start] = topic.Repos[i]
			}
		}
		prior[topic.ID+"\n"+topic.Query.GitHub] = counts
	}
	return prior
}
