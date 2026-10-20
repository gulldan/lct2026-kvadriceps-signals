package postgres

// Создано командой bun run taxonomy:generate; источник — researchTaxonomy.json.
const documentTopicMethod = "title_rules_v3"

func documentTopicLabel(id string) string {
	return map[string]string{
		"energy":               "Энергетика",
		"fashion":              "Мода и текстиль",
		"fintech":              "Финансовые технологии",
		"food":                 "Пищевые технологии",
		"gaming":               "Игровые технологии",
		"medicine":             "Медицина и здоровье",
		"transport":            "Транспорт и логистика",
		"hosting":              "Облачная инфраструктура и хранение данных",
		"real_estate":          "Недвижимость",
		"robotics":             "Робототехника",
		"marketing":            "Маркетинг",
		"media":                "Медиа и цифровой контент",
		"legal":                "Правовые технологии",
		"telecom":              "Телекоммуникации",
		"jobs_recruitment":     "Кадры и подбор персонала",
		"home_living":          "Умный дом и быт",
		"education":            "Образование",
		"computing":            "Корпоративное ПО и вычисления",
		"dating":               "Знакомства и поиск партнёра",
		"event_tech":           "Технологии мероприятий",
		"semiconductors":       "Полупроводники",
		"wellness_beauty":      "Благополучие и красота",
		"kids":                 "Детские товары и сервисы",
		"music":                "Музыка",
		"travel":               "Туризм и путешествия",
		"space":                "Космос и астрономия",
		"sports":               "Спорт",
		"consumer_electronics": "Потребительская электроника",
		"security":             "Кибербезопасность и защита",
		"materials":            "Химия и материалы",
		"industry":             "Инженерия и промышленное оборудование",
		"agriculture":          "Сельское хозяйство",
		"water":                "Вода и водоочистка",
		"environment":          "Климат и экология",
		"biotechnology":        "Биотехнологии",
		"economics":            "Экономика и общество",
		"science":              "Фундаментальные науки",
	}[id]
}
