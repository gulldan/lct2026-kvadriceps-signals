package application

import (
	"strings"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

type experimentDomainSpec struct {
	Type     string
	Label    string
	Guidance string
	Sections []experimentSectionSpec
}

type experimentSectionSpec struct {
	Title    string
	Purpose  string
	Guidance string
}

func buildExperimentPrompt(h *commonv1.Hypothesis) string {
	var b strings.Builder
	spec := experimentDomainSpecForHypothesis(h)
	b.WriteString("Гипотеза: «")
	b.WriteString(h.GetStatement())
	b.WriteString("».")
	if d := compactText(h.GetDetail()); d != "" {
		b.WriteString("\nДанные гипотезы (паспорт, JSON): ")
		b.WriteString(d)
	}
	b.WriteString("\n\nТы — инженер-экспериментатор НИОКР. Опираясь на гипотезу и приведённый выше контекст " +
		"(выдержки из документов с методиками и условиями), составь КОНКРЕТНЫЙ план эксперимента, который " +
		"подтвердит или опровергнет гипотезу. Сначала выбери experiment_type: new_alloy, process_route, " +
		"coating_corrosion, battery_material, ore_beneficiation (обогащение руды: дробление/измельчение/" +
		"флотация/гравитация/магнитная сепарация), metallurgy_process (плавка/выщелачивание/электролиз/обжиг) " +
		"или generic. Добавь sections — предметные этапы именно для " +
		"этого типа исследования, а не универсальный чеклист. Укажи: материалы/реактивы; параметры процесса с диапазонами " +
		"значений; методы характеризации и испытаний; контроли/базовый образец для сравнения; ИЗМЕРИМЫЕ " +
		"критерии успеха; ориентировочную стоимость (low|medium|high); ориентировочный срок (days|weeks|months); " +
		"ключевые риски. Не выдумывай числа, которых нет в контексте, кроме явно предлагаемых диапазонов варьирования.")
	b.WriteString("\nРекомендуемая предметная специализация: experiment_type=")
	b.WriteString(spec.Type)
	b.WriteString(" (")
	b.WriteString(spec.Label)
	b.WriteString("). ")
	b.WriteString(spec.Guidance)
	b.WriteString("\nОжидаемые sections для этого домена:")
	for _, section := range spec.Sections {
		b.WriteString("\n- ")
		b.WriteString(section.Title)
		b.WriteString(": ")
		b.WriteString(section.Purpose)
		if section.Guidance != "" {
			b.WriteString(" ")
			b.WriteString(section.Guidance)
		}
	}
	b.WriteString(untrustedContextInstruction)
	b.WriteString(langInstruction)
	b.WriteString("\nВерни СТРОГО JSON без markdown:\n")
	b.WriteString(`{"experiment_type":"new_alloy|process_route|coating_corrosion|battery_material|` +
		`ore_beneficiation|metallurgy_process|generic",` +
		`"sections":[{"title":"этап","purpose":"зачем нужен этап","items":["конкретное действие"]}],` +
		`"materials":["материал/реактив"],` +
		`"process_parameters":[{"name":"параметр","range":"диапазон значений с единицами"}],` +
		`"characterization_methods":["SEM","XRD"],"test_methods":["tensile test"],` +
		`"controls":["базовый образец/контроль"],"success_criteria":["измеримый критерий"],` +
		`"estimated_cost":"low|medium|high","estimated_time":"days|weeks|months","risks":["ключевой риск"]}`)
	return b.String()
}

func experimentDomainSpecForHypothesis(h *commonv1.Hypothesis) experimentDomainSpec {
	return experimentDomainSpecForText(h.GetTitle() + " " + h.GetStatement() + " " + h.GetDetail())
}

func experimentDomainSpecForText(text string) experimentDomainSpec {
	text = strings.ToLower(text)
	switch {
	case containsAny(text, "катод", "анод", "battery", "cell", "ёмкост", "емкост"):
		return experimentDomainSpec{
			Type:     expTypeBatteryMaterial,
			Label:    "батарейный материал",
			Guidance: "План должен отделять синтез материала от изготовления электрода и электрохимической проверки.",
			Sections: batteryMaterialSections(),
		}
	case containsAny(text, "корроз", "coating", "покрыт", "защитн"):
		return experimentDomainSpec{
			Type:     expTypeCoatingCorrosion,
			Label:    "покрытие и коррозия",
			Guidance: "План должен явно разделять подготовку образца, нанесение покрытия и коррозионное испытание.",
			Sections: coatingCorrosionSections(),
		}
	case containsAny(text, "выщелач", "электролиз", "обжиг", "металлург"):
		return experimentDomainSpec{
			Type:     expTypeMetallurgy,
			Label:    "металлургический передел",
			Guidance: "План должен идти от шихтовки к режиму агрегата, контролю состава и балансу металла.",
			Sections: metallurgyProcessSections(),
		}
	case containsAny(text, "флотац", "обогащ", "измельчен", "гравитац", "магнитн", "концентрат", "хвост", "извлечен"):
		return experimentDomainSpec{
			Type:     expTypeOreBeneficiation,
			Label:    "обогащение руды",
			Guidance: "План должен идти от характеристики питания к схеме опыта, реагентному режиму и балансу металла.",
			Sections: oreBeneficiationSections(),
		}
	case containsAny(text, "сплав", "alloy", "плав", "лить"):
		return experimentDomainSpec{
			Type:     expTypeNewAlloy,
			Label:    "новый сплав",
			Guidance: "План должен идти от состава и шихты к плавке, термообработке, микроструктуре и испытаниям.",
			Sections: newAlloySections(),
		}
	case containsAny(text, "маршрут", "process", "режим", "сверл", "обработ", "бурен", "drill"):
		return experimentDomainSpec{
			Type:     expTypeProcessRoute,
			Label:    "технологический маршрут",
			Guidance: "План должен сравнивать новый маршрут с базовым и фиксировать окно режимов, качество и производительность.",
			Sections: processRouteSections(),
		}
	default:
		return experimentDomainSpec{
			Type:     expTypeGeneric,
			Label:    "общий НИОКР-план",
			Guidance: "Если домен не очевиден, sections всё равно должны вести от объекта и воздействия к измерению свойства и KPI.",
			Sections: genericSections(),
		}
	}
}

func batteryMaterialSections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{
			"Синтез материала",
			"получить порошок/прекурсор заданного состава",
			"Укажи исходные соли/прекурсоры, температуру/атмосферу и контроль фазового состава.",
		},
		{
			"Изготовление электрода",
			"сделать воспроизводимый электрод",
			"Укажи связующее, проводящую добавку, загрузку и сушку, если это есть в контексте.",
		},
		{
			"Сборка ячейки",
			"зафиксировать конфигурацию испытания",
			"Опиши тип ячейки, электролит, сепаратор и контрольный электрод без выдуманных чисел.",
		},
		{
			"Циклирование и диагностика",
			"измерить ёмкость, удержание и деградацию",
			"Включи EIS/CV/galvanostatic cycling, если они поддержаны источниками.",
		},
	}
}

func coatingCorrosionSections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{
			"Подготовка подложки",
			"сделать сравнимые образцы",
			"Укажи материал, очистку/шероховатость и контрольную серию.",
		},
		{
			"Нанесение покрытия",
			"получить покрытие заданного состава и толщины",
			"Опиши метод нанесения, температуру/время/атмосферу и диапазоны варьирования.",
		},
		{
			"Контроль покрытия",
			"проверить структуру и адгезию до коррозии",
			"Включи толщину, пористость, SEM/EDS/XRD, scratch/adhesion test по контексту.",
		},
		{
			"Коррозионная проверка",
			"измерить скорость коррозии и механизм отказа",
			"Укажи среду, температуру, время, EIS/поляризацию/массовые потери, если они есть в источниках.",
		},
	}
}

func newAlloySections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{
			"Состав и шихта",
			"зафиксировать химический состав и чистоту исходных материалов",
			"Укажи элементы/лигатуры, допуски и контроль состава.",
		},
		{
			"Плавка и получение образцов",
			"получить слиток/заготовку без смешения с проверкой свойства",
			"Опиши вакуум/инертную атмосферу, способ плавки/литья и число повторов, если поддержано контекстом.",
		},
		{
			"Термообработка или деформация",
			"сформировать требуемую микроструктуру",
			"Укажи температуру, время, охлаждение и диапазоны варьирования.",
		},
		{
			"Микроструктурный контроль",
			"проверить фазовый состав, зерно и выделения",
			"Включи SEM/TEM/XRD/DSC/EBSD только если это уместно для гипотезы.",
		},
		{
			"Механические испытания",
			"связать микроструктуру с KPI",
			"Укажи tensile/creep/hardness/fatigue и температуру испытания, если она есть в цели или источнике.",
		},
	}
}

func processRouteSections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{
			"Базовый маршрут",
			"задать контрольный процесс для сравнения",
			"Опиши текущий режим, оборудование и измеряемый KPI.",
		},
		{
			"Изменяемые параметры",
			"выделить управляемые факторы процесса",
			"Для плавки, обработки, сверления или бурения укажи только параметры, поддержанные контекстом.",
		},
		{
			"Окно режимов",
			"проверить диапазоны без подмены результата предположениями",
			"Задай диапазоны варьирования и ограничения безопасности/оборудования.",
		},
		{
			"Контроль качества",
			"измерить дефекты, структуру и стабильность",
			"Опиши методы контроля, браковочные признаки и повторяемость.",
		},
		{
			"Сравнение с базой",
			"оценить прирост по KPI и побочные эффекты",
			"Сравни производительность, брак, стоимость, ресурс или свойство в зависимости от цели.",
		},
	}
}

func oreBeneficiationSections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{
			"Характеристика питания",
			"зафиксировать состав и свойства исходного сырья",
			"Укажи гранулометрию, содержания металлов и минеральные формы из контекста.",
		},
		{
			"Схема опыта",
			"задать лабораторную схему обогащения",
			"Опиши лабораторную флотацию/гравитацию/магнитную сепарацию, число стадий и контрольную серию.",
		},
		{
			"Реагентный режим",
			"выделить управляемые факторы процесса",
			"Укажи реагенты, расходы, pH и время только из контекста, диапазоны — как варьирование.",
		},
		{
			"Баланс металла",
			"свести извлечение и качество по продуктам",
			"Посчитай выход, содержание и извлечение по концентрату и хвостам.",
		},
		{
			"Критерии извлечения",
			"связать результат опыта с KPI",
			"Задай измеримый порог по извлечению/качеству концентрата против базовой схемы.",
		},
	}
}

func metallurgyProcessSections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{
			"Шихтовка",
			"зафиксировать состав шихты/раствора и исходные материалы",
			"Укажи компоненты, соотношения и контроль состава по контексту.",
		},
		{
			"Режим агрегата",
			"задать управляемые параметры передела",
			"Для плавки, выщелачивания, электролиза или обжига укажи температуру/время/расходы как диапазоны варьирования.",
		},
		{
			"Контроль состава",
			"проверить продукты передела",
			"Опиши пробоотбор и анализ состава продуктов и полупродуктов.",
		},
		{
			"Баланс металла",
			"оценить извлечение и потери против базы",
			"Сведи баланс по металлу и сравни извлечение и затраты с базовым режимом.",
		},
	}
}

func genericSections() []experimentSectionSpec {
	return []experimentSectionSpec{
		{"Объект и контроль", "описать материал/образец и базовую серию", "Укажи, с чем сравнивается гипотеза."},
		{"Варьируемое воздействие", "задать управляемые факторы", "Не придумывай диапазоны, которых нет в контексте."},
		{"Методы измерения", "проверить заявленное свойство", "Раздели характеризацию и испытания."},
		{"Критерий решения", "понять, подтверждена гипотеза или нет", "Критерий должен быть измеримым и связанным с KPI."},
	}
}
