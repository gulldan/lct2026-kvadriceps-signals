package application

import (
	"context"
	"strings"
	"testing"
)

func TestTechnologyBriefWriterKeepsRussianFieldsOnly(t *testing.T) {
	on := func(context.Context) bool { return true }
	in := TechnologyBriefInput{TechnologyID: "t1", Name: "Nonce challenge",
		Sources: []TechnologyBriefSource{{Title: "Paper", Quotes: []string{"replay acceptance fell from 20% to 2%"}}}}
	reply := "```json\n{\"title\":\"Внедрение одноразовых заданий\",\"advantage\":\"Better than passive detection\"," +
		"\"problem\":\"Повторное воспроизведение речи обходит проверку голоса.\"," +
		"\"hypothesis\":\"Если ввести одноразовые задания, то доля принятых повторов снизится, потому что запись " +
		"не содержит ответа.\",\"grounds\":\"Основания: в испытании доля повторов упала с 20 до 2 процентов.\"," +
		"\"method\":\"Пилот на одном канале с контрольной группой.\"," +
		"\"criterion\":\"Снижение доли повторов вдвое поддержит гипотезу.\"}\n```"
	brief, err := NewTechnologyBriefWriter(fakeGenerator{reply}, on)(t.Context(), in)
	if err != nil || brief.TechnologyID != "t1" || brief.Sources != 1 || brief.Advantage != "" ||
		brief.Title != "Внедрение одноразовых заданий" || !strings.HasPrefix(brief.Hypothesis, "Если ") ||
		brief.WrittenAt == "" || !strings.Contains(brief.Basis, "1") ||
		!strings.HasPrefix(brief.Grounds, "в испытании") {
		t.Fatalf("brief %+v, err %v", brief, err)
	}
	if _, err := NewTechnologyBriefWriter(fakeGenerator{`{"title":"Название","hypothesis":""}`}, on)(
		t.Context(), in); err == nil {
		t.Fatal("бриф без гипотезы принят")
	}
}
