package application

import "testing"

func TestLiveFamilyIndustriesCoverEveryFamily(t *testing.T) {
	industries := liveFamilyIndustries()
	known := map[string]bool{}
	for _, rubric := range liveRubrics() {
		known[rubric.ID] = true
	}
	families := liveFamilies()
	for _, family := range families {
		if !known[industries[family.id]] {
			t.Errorf("%s: отрасль %q не из каталога рубрик", family.id, industries[family.id])
		}
	}
	if len(industries) != len(families) {
		t.Errorf("в таблице отраслей %d направлений, а семейств %d: лишний или опечатанный id", len(industries), len(families))
	}
}
