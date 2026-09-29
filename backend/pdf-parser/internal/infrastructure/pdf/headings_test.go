package pdf

import "testing"

func TestDetectHeading(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"numbered ru", "1 Введение", true},
		{"numbered dotted ru", "2.3.1 Метод расчёта", true},
		{"numbered trailing dot", "1. Введение", true},
		{"numbered en", "3 Results", true},
		{"numbered deep", "4.5.6.7 Граничные условия", true},
		{"numbered but sentence", "1. Это длинное предложение, которое заканчивается точкой.", false},
		{"numbered long no punct flows like prose",
			"5 В этой части мы подробно рассматриваем все возможные источники " +
				"систематической ошибки измерения", false},

		{"keyword glava", "Глава 2. Теоретические основы", true},
		{"keyword razdel", "Раздел 1", true},
		{"keyword prilozhenie", "Приложение А", true},
		{"keyword vvedenie", "Введение", true},
		{"keyword zaklyuchenie", "Заключение", true},
		{"keyword spisok", "Список литературы", true},
		{"keyword chapter", "Chapter 4", true},
		{"keyword appendix lower", "appendix b", true},
		{"keyword references", "References", true},
		{"keyword section sign", "§ 12 Ответственность сторон", true},
		{"keyword as substring not prefix", "В разделе ниже мы покажем, что метод сходится.", false},

		{"allcaps ru", "ВВЕДЕНИЕ", true},
		{"allcaps en", "MATERIALS AND METHODS", true},
		{"title case ru", "Метод Конечных Элементов", true},
		{"title case en", "Results And Discussion", true},
		{"single capitalized word", "Аннотация", true},

		{"body sentence ru", "Первая страница отчёта.", false},
		{"body sentence en", "This is a normal sentence that ends with a period.", false},
		{"short sentence with period", "Это так.", false},
		{"lowercase short phrase", "далее по тексту", false},
		{"ends with comma", "во-первых,", false},
		{"ends with colon", "Таблица параметров:", false},
		{"long title-case paragraph over 80 runes",
			"Очень Длинный Заголовок Который На Самом Деле Является Абзацем И " +
				"Превышает Восемьдесят Рун Поэтому Не Считается", false},
		{"many words not heading", "Один Два Три Четыре Пять Шесть Семь Восемь Девять Десять Одиннадцать Двенадцать Тринадцать", false},
		{"sentence-case prose no punct", "это обычный текст без заглавных букв и без точки", false},
		{"empty", "", false},
		{"whitespace only", "   ", false},

		{"caption ris", "Рис. 3", false},
		{"caption risunok dash", "Рисунок 3 — Схема установки", false},
		{"caption tablica", "Таблица 2", false},
		{"caption tablica dash", "Таблица 2 — Результаты измерений", false},
		{"caption figure", "Figure 3", false},
		{"caption fig dot", "Fig. 3", false},
		{"caption table en", "Table 2", false},
		{"date ru lowercase month", "12 марта 2024 года", false},
		{"place and year", "Москва, 2024", false},
		{"place and country comma", "Москва, Россия", false},
		{"author list comma", "Иванов, Петров и Сидоров", false},
		{"clause comma title", "Во-первых, Метод", false},
		{"single inflected word vvedeniem", "Введением", false},
		{"single inflected word razdele", "Разделе", false},
		{"lone proper noun", "Москва", false},

		{"numbered upper still heading", "2 Методология", true},
		{"allcaps single still heading", "РЕЗУЛЬТАТЫ", true},
		{"title case two words still heading", "Граничные Условия", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := detectHeading(c.in); got != c.want {
				t.Errorf("detectHeading(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
