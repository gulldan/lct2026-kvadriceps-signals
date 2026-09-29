package application

import (
	"errors"
	"testing"
)

func TestLiveCoverTakesOnlyPathsInsideThePaper(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"https://evil.example/x.png", "//evil.example/x.png", "/html/2508.05660v1/x.png",
		"../2508.00001v1/x.png", "2508.056601/x.png", "2508.05660v1/%2e%2e/%2e%2e/x.png",
		"2508.05660v1/x.svg", "data:image/png;base64,AAAA", `2508.05660v1\..\x.png`,
	} {
		if got, ok := liveCoverImageURL("2508.05660", src); ok {
			t.Errorf("%q accepted as %q", src, got)
		}
	}
	page := `<figure class="ltx_figure"><img src="http://10.0.0.1/x.png"></figure>
<figure class="ltx_figure"><img src="2508.05660v1/figs/method.PNG">
<figcaption><span class="ltx_tag ltx_tag_figure">Figure 2. </span>Overview of <math alttext="k"></math> steps.` +
		`<span class="ltx_nodisplay">hidden</span></figcaption></figure>`
	src, number, caption, ok := liveCoverFigure([]byte(page), "2508.05660")
	if !ok || src != "https://arxiv.org/html/2508.05660v1/figs/method.PNG" || number != "2" || caption != "Overview of k steps." {
		t.Fatalf("figure = %q %q %q %v", src, number, caption, ok)
	}
}

func TestLivePaperCoverRejectsForeignIDs(t *testing.T) {
	t.Parallel()
	covers := NewLiveCovers(nil)
	for _, id := range []string{"doi:10.1/x", "arxiv:../../etc", "https://evil.example/a.png", ""} {
		if _, err := covers.PaperCover(t.Context(), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("PaperCover(%q) = %v, want ErrNotFound", id, err)
		}
	}
}
