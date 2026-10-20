package application

import "strings"

// liveMoneyRegions — макрорегионы карты мира по тем же спискам, что у scripts/world-map/build.py: страна на карте
// красится тем же регионом, в котором считаются её сделки. Территории, которых нет в контуре 110m, — в конце строк.
func liveMoneyRegions() map[string]string {
	table := map[string]string{}
	for region, codes := range map[string]string{
		liveMoneyRegionUS: "CA GL MX US BM",
		liveMoneyRegionLA: "AR BO BR BS BZ CL CO CR CU DO EC FK GT GY HN HT JM NI PA PE PR PY SR SV TT UY VE AG BB KY LC VC VG",
		liveMoneyRegionEU: "AL AT BA BE BG BY CH CY CZ DE DK EE ES FI FR GB GR HR HU IE IS IT LT LU LV MD ME MK NL NO PL PT RO RS " +
			"SE SI SK UA XK AD FO GG GI IM JE LI MC MT SM VA",
		liveMoneyRegionCIS: "AM AZ GE KG KZ RU TJ TM UZ",
		liveMoneyRegionME:  "AE BH DZ EG EH IL IQ IR JO KW LB LY MA OM PS QA SA SY TN TR YE",
		liveMoneyRegionAF: "AO BF BI BJ BW CD CF CG CI CM DJ ER ET GA GH GM GN GQ GW KE LR LS MG ML MR MW MZ NA NE NG RW SD " +
			"SL SN SO SS SZ TD TG TZ UG ZA ZM ZW CV KM MU SC ST",
		liveMoneyRegionAS: "AF AU BD BN BT CN FJ ID IN JP KH KP KR LA LK MM MN MY NC NP NZ PG PH PK SB TH TL TW VN VU HK MO MV SG",
	} {
		for code := range strings.FieldsSeq(codes) {
			table[code] = region
		}
	}
	return table
}

// liveMoneyCountry приводит код к ISO alpha-2: CORDIS пишет EL и UK вместо GR и GB.
func liveMoneyCountry(code string) string {
	switch code = strings.ToUpper(strings.TrimSpace(code)); code {
	case "EL":
		return "GR"
	case "UK":
		return "GB"
	}
	return code
}

// liveMoneyRegion ставит регион по стране компании; без страны остаётся регион источника.
func liveMoneyRegion(regions map[string]string, country, fallback string) string {
	if region := regions[liveMoneyCountry(country)]; region != "" {
		return region
	}
	return fallback
}

// liveMoneyDomainCountry — национальный домен компании: единственный след страны у пресс-записей набора.
// ponytail: «вольные» .ai/.io/.co/.me не считаются; это даёт страну ~4 % пресс-раундов, остальным нужно поле в наборе.
func liveMoneyDomainCountry(domain string) string {
	tld := strings.ToUpper(domain[strings.LastIndex(domain, ".")+1:])
	national := " AR AT AU BD BE BG BR CA CH CL CN CZ DE DK EE EG ES FI FR GR HK HU ID IE IL IN IT JP KE KR KZ LK LT LU LV MX MY" +
		" NG NL NO NZ PE PH PK PL PT QA RO RS RU SA SE SG SI SK TH TR TW UA UK US VN ZA "
	if tld == "" || !strings.Contains(national, " "+tld+" ") {
		return ""
	}
	return liveMoneyCountry(tld)
}
