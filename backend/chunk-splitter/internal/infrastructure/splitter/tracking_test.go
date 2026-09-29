package splitter

import (
	"testing"
)

// initials — константа, только чтобы повтор «И.» не ловил dupword; точки разбивают однобуквенные
// токены, и CollapseTracking не должен это трогать.
const initials = "И." + " " + "И." + " Иванов"

// hasTrackedRun берёт производственный tokenize, чтобы проверка следила ровно за тем,
// с чем работает CollapseTracking.
func hasTrackedRun(s string) bool {
	tokens, gaps := tokenize(s)
	for i := 0; i < len(tokens); {
		if n := trackedRunLength(tokens, gaps, i); n >= minTrackedRun {
			return true
		} else if n > 1 {
			i += n
		} else {
			i++
		}
	}
	return false
}

func TestCollapseTracking_Table(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "clean single tracked word",
			in:   "М е т о д о л о г и я",
			want: "Методология",
		},
		{
			name: "normal text unchanged",
			in:   "кот и пёс гуляли вместе",
			want: "кот и пёс гуляли вместе",
		},
		{
			name: "initials not collapsed (periods break single-letter tokens)",
			in:   initials,
			want: initials,
		},
		{
			name: "short spaced run below threshold unchanged",
			in:   "a b c d",
			want: "a b c d",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "exactly threshold letters collapse",
			in:   "а б в г д",
			want: "абвгд",
		},
		{
			name: "four letters below threshold unchanged",
			in:   "а б в г",
			want: "а б в г",
		},
		{
			name: "double-space gap still glues (PDF tracking uses 1-2 ws)",
			in:   "а б в г д  е ж з и к",
			want: "абвгдежзик",
		},
		{
			name: "gap over the connector limit (3 ws) ends the run",
			in:   "а б в г д   е ж з и к",
			want: "абвгд   ежзик",
		},
		{
			name: "paragraph break preserved around collapsed run",
			in:   "слово\n\nа б в г д е\n\nещё",
			want: "слово\n\nабвгде\n\nещё",
		},
		{
			name: "newline-joined run collapses",
			in:   "т е х н о л о г и я",
			want: "технология",
		},
		{
			name: "leading and trailing whitespace preserved",
			in:   "  а б в г д  ",
			want: "  абвгд  ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CollapseTracking(tc.in); got != tc.want {
				t.Fatalf("CollapseTracking(%q)\n  got  = %q\n  want = %q", tc.in, got, tc.want)
			}
		})
	}
}

// Границы слов внутри разреженного текста не восстановить: это те же пробелы, что и между буквами.
// Поэтому проверяются только отсутствие разреженного участка и наличие настоящих слов.
func TestCollapseTracking_RealMixedSample(t *testing.T) {
	in := "в М\nе т\nо д\nо л\nо г\nи я и\n т е\nх н\nо л\nо г\nи я"
	got := CollapseTracking(in)

	if hasTrackedRun(got) {
		t.Fatalf("tracking should be gone, but a >=%d single-letter run survives: %q", minTrackedRun, got)
	}
	for _, sub := range []string{"етодологи", "ехнологи"} {
		if indexString(got, sub) < 0 {
			t.Fatalf("expected substring %q in result %q", sub, got)
		}
	}
}

func TestCollapseTracking_Idempotent(t *testing.T) {
	inputs := []string{
		"М е т о д о л о г и я",
		"в М\nе т\nо д\nо л\nо г\nи я и\n т е\nх н\nо л\nо г\nи я",
		"кот и пёс гуляли вместе",
		initials,
		"a b c d",
		"слово\n\nа б в г д е\n\nещё",
		"  а б в г д  ",
	}
	for _, in := range inputs {
		once := CollapseTracking(in)
		twice := CollapseTracking(once)
		if once != twice {
			t.Fatalf("not idempotent for %q:\n  f(x)    = %q\n  f(f(x)) = %q", in, once, twice)
		}
	}
}

func indexString(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
