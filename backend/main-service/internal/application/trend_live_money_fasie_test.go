package application

import (
	"strings"
	"testing"
)

func TestLiveMoneyFSIParsesListingAndWinners(t *testing.T) {
	t.Parallel()
	rules := liveMoneyFSIRules()
	listing := `<li><h5><a href="/press/fund/start-1-2026-grafik/">ГРАФИКИ ЗАСЕДАНИЯ</a></h5><p><b>02.10.2026</b></p></li>
<li><h2><a href="/press/fund/razvitie-bio-results/">ПОДВЕДЕНЫ ИТОГИ ПО КОНКУРСУ «РАЗВИТИЕ-БИО» (ОЧЕРЕДЬ 1)</a> </h2>
<p><span class="ico_clock"><b>05.10.2026</b></span> </p></li>`
	pages := liveMoneyFSIListing(rules, []byte(listing))
	if len(pages) != 1 || pages[0].Date != "2026-10-05" || pages[0].URL != "https://fasie.ru/press/fund/razvitie-bio-results/" ||
		!strings.HasPrefix(pages[0].Title, "ПОДВЕДЕНЫ ИТОГИ") {
		t.Fatalf("listing: %+v", pages)
	}
	detail := `<div itemprop="text"><p>Фондом подведены итоги конкурса «Развитие-Био» (очередь 1).</p>
<p>По результатам конкурсного отбора 2 заявки на сумму 59 млн рублей рекомендованы к финансированию и 1 заявка на
сумму 16 млн рублей рекомендована к финансированию при появлении средств.</p>
<p><a href="/upload/docs/R-bio-results.docx">Список победителей</a></p></div><div>п.п. 1.8 рекомендованы</div>`
	cell := func(text string) string { return "<w:tc><w:p><w:r><w:t>" + text + "</w:t></w:r></w:p></w:tc>" }
	row := func(cells ...string) string { return "<w:tr>" + strings.Join(cells, "") + "</w:tr>" }
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:tbl>` +
		row(cell("№"), cell("№ Заявки"), cell("Название проекта"), "<w:tc><w:p><w:r><w:t>Размер гранта,</w:t></w:r>"+
			"<w:r><w:t>руб.</w:t></w:r></w:p></w:tc>") +
		row(cell("1"), cell("РБио-644313"), cell("Инструментарий анализа геномных данных скота"), cell("30 000 000")) +
		row(cell("2"), cell("РБио-644674"), cell("Платформа культивирования штаммов"), cell("28 900 000")) +
		`</w:tbl><w:p><w:r><w:t>при наличии средств</w:t></w:r></w:p><w:tbl>` + row(cell("резерв")) + `</w:tbl></w:body></w:document>`
	events := liveMoneyFSIEvents(rules, liveRubrics(), pages[0], []byte(detail), []byte(document))
	if len(events) != 2 || events[0].Amount != "30 млн ₽" || events[1].Amount != "28,9 млн ₽" || events[0].ID == events[1].ID ||
		events[0].Investors != "ФСИ, Развитие-Био" || events[0].Rubric != "biotechnology" || events[0].Country != "RU" ||
		events[0].Quote != "Инструментарий анализа геномных данных скота" || events[0].Title != pages[0].Title {
		t.Fatalf("winners: %+v", events)
	}
	fallback := liveMoneyFSIEvents(rules, liveRubrics(), pages[0], []byte(detail), nil)
	if len(fallback) != 1 || fallback[0].Amount != "59 млн ₽" || fallback[0].Company != "" ||
		!strings.HasPrefix(fallback[0].Quote, "По результатам конкурсного отбора 2 заявки") {
		t.Fatalf("competition-level fallback: %+v", fallback)
	}
}
