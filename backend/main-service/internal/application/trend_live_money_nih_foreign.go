package application

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strconv"
)

// NIH финансирует и организации за пределами США — больше всего в Африке. Это научный грант, а не деньги
// компании, поэтому у события свой вид. Фильтр «не США» API не умеет: страны перечисляются по названию.

const liveMoneyNIHForeignSource = "NIH RePORTER: гранты организациям вне США"

// liveMoneyNIHCountries — страны в написании RePORTER (сокращённые имена FIPS: «TANZANIA U REP», «BURKINA»).
// ponytail: список — страны получателей за 10.2025–10.2026 и очевидные однословные имена; страна с иным
// написанием теряется, пока её не добавят сюда.
func liveMoneyNIHCountries() map[string]string {
	return map[string]string{
		"UNITED KINGDOM": "GB", "CANADA": "CA", "AUSTRALIA": "AU", "SOUTH AFRICA": "ZA", "UGANDA": "UG", "KENYA": "KE",
		"PERU": "PE", "THAILAND": "TH", "NIGERIA": "NG", "MALI": "ML", "TANZANIA U REP": "TZ", "GHANA": "GH",
		"ARGENTINA": "AR", "FRANCE": "FR", "BOTSWANA": "BW", "MALAWI": "MW", "SWITZERLAND": "CH", "NETHERLANDS": "NL",
		"BRAZIL": "BR", "PAKISTAN": "PK", "INDIA": "IN", "GERMANY": "DE", "VIETNAM": "VN", "CAMEROON": "CM",
		"CAMBODIA": "KH", "SWEDEN": "SE", "ETHIOPIA": "ET", "CONGO DEM REP": "CD", "ZAMBIA": "ZM", "RWANDA": "RW",
		"ZIMBABWE": "ZW", "GEORGIA": "GE", "MOZAMBIQUE": "MZ", "DENMARK": "DK", "MALAYSIA": "MY", "BANGLADESH": "BD",
		"BELGIUM": "BE", "GAMBIA": "GM", "COSTA RICA": "CR", "FINLAND": "FI", "HAITI": "HT", "AUSTRIA": "AT",
		"LIBERIA": "LR", "IRELAND": "IE", "ICELAND": "IS", "ECUADOR": "EC", "MONGOLIA": "MN", "LEBANON": "LB",
		"COLOMBIA": "CO", "EGYPT": "EG", "JORDAN": "JO", "TUNISIA": "TN", "ITALY": "IT", "BURKINA": "BF", "GREECE": "GR",
		"CHINA": "CN", "JAPAN": "JP", "ISRAEL": "IL", "SPAIN": "ES", "NORWAY": "NO", "PORTUGAL": "PT", "CHILE": "CL",
		"MEXICO": "MX", "GUATEMALA": "GT", "INDONESIA": "ID", "PHILIPPINES": "PH", "NEPAL": "NP", "SENEGAL": "SN",
		"SINGAPORE": "SG", "TAIWAN": "TW", "NEW ZEALAND": "NZ", "TURKEY": "TR", "BENIN": "BJ", "NAMIBIA": "NA",
		"POLAND": "PL", "HUNGARY": "HU", "ROMANIA": "RO", "ARMENIA": "AM", "KAZAKHSTAN": "KZ", "UKRAINE": "UA",
	}
}

// liveMoneyNIHForeign берёт решения о награде в окне, включая продления: деньги пришли в страну в этот срок.
// Реестр отдаёт и гранты FDA и CDC; они пропускаются, чтобы «NIH» в программе было правдой.
func liveMoneyNIHForeign(ctx context.Context, client *http.Client, rubrics []liveRubric,
	from, through string) ([]LiveMoneyEvent, int, error) {
	countries := liveMoneyNIHCountries()
	var query liveMoneyNIHRequest
	query.Criteria.OrgCountries = slices.Sorted(maps.Keys(countries))
	query.Criteria.NoticeDate = map[string]string{"from_date": from, "to_date": through}
	query.Criteria.ExcludeSubprojects = true
	query.IncludeFields = liveMoneyNIHFields()
	events := []LiveMoneyEvent{}
	total, err := liveMoneyNIHSearch(ctx, client, query, func(row liveMoneyNIHProject) {
		if row.AgencyCode == "NIH" {
			events = append(events, liveMoneyNIHForeignEvent(rubrics, countries, row))
		}
	})
	return events, total, err
}

func liveMoneyNIHForeignEvent(rubrics []liveRubric, countries map[string]string, row liveMoneyNIHProject) LiveMoneyEvent {
	event := liveMoneyGrant(rubrics, liveMoneyNIHForeignSource, row.Title, row.Abstract)
	event.Kind, event.Stage = LiveMoneyResearch, LiveMoneyResearch
	event.ID = liveMoneyID("nih", strconv.FormatInt(row.ApplID, 10))
	event.Company, event.Date, event.URL = row.Organization.Name, row.Notice[:min(len(row.Notice), 10)], row.URL
	event.Country, event.Region, event.Investors = countries[row.Organization.Country], liveMoneyUnknown, "NIH, "+row.ActivityCode
	if row.Amount > 0 {
		event.Amount = liveMoneyUSD(float64(row.Amount))
	}
	return event
}
