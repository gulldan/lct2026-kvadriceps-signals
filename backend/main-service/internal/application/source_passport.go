package application

import (
	"cmp"
	"maps"
	"net/url"
	"strings"
)

// SourcePassport — кто опубликовал запись и насколько на неё можно опереться. Соцсети, блоги, агрегаторы,
// реклама и пресс-релизы по постановке — лишь первичный индикатор, поэтому доверие у них пониженное:
// без независимого подтверждения технология по ним в список не входит.
type SourcePassport struct {
	Publisher  string `json:"publisher_type"`
	Trust      string `json:"trust"`
	TrustBasis string `json:"trust_basis"`
}

const (
	TrustHigh    = "high"
	TrustMedium  = "medium"
	TrustLowered = "lowered"
)

const (
	PublisherGovernment   = "government"
	PublisherRegulator    = "regulator"
	PublisherInterOrg     = "international_org"
	PublisherUniversity   = "university"
	PublisherResearch     = "research_center"
	PublisherJournal      = "journal"
	PublisherPatentOffice = "patent_office"
	PublisherRegistry     = "state_registry"
	PublisherAssociation  = "industry_association"
	PublisherVendor       = "vendor"
	PublisherMedia        = "industry_media"
	PublisherPreprint     = "preprint_server"
	PublisherCodeHost     = "code_host"
	PublisherAggregator   = "aggregator"
	PublisherBlog         = "blog"
	PublisherSocial       = "social_network"
	PublisherUnknown      = "unknown"
)

const (
	basisPatentOffice     = "патентное ведомство"
	basisPeerJournal      = "рецензируемый научный журнал"
	basisUniversity       = "университет"
	basisPreprint         = "препринт без рецензирования"
	basisBibliographic    = "агрегатор библиографических метаданных"
	basisStandards        = "организация по стандартизации"
	basisAssociation      = "отраслевая ассоциация"
	basisVendorResearch   = "исследовательский блог компании-разработчика"
	basisVendorOfficial   = "официальный материал компании-разработчика"
	basisVendorNewsroom   = "пресс-центр или продуктовый блог компании: пресс-релизы"
	basisBusinessMedia    = "деловое медиа"
	basisVentureMedia     = "издание о венчурном рынке"
	basisNewsAgency       = "информационное агентство"
	basisTradeMedia       = "профессиональное отраслевое медиа"
	basisIndustryMedia    = "отраслевое медиа"
	basisBlogPlatform     = "площадка личных блогов"
	basisSocial           = "социальная сеть"
	basisPopularScience   = "научно-популярное медиа"
	basisPressWire        = "лента пресс-релизов"
	basisScienceWire      = "агрегатор научных пресс-релизов"
	basisCommunityForum   = "форум сообщества без рецензирования"
	basisAuthorNewsletter = "авторская рассылка без редакционной проверки"
	basisGovDomain        = "государственный домен"
	basisEduDomain        = "университетский домен"
	basisPackageRegistry  = "реестр пакетов: пакет публикует сам автор, скачивания считает реестр"
)

type passportRule struct {
	publisher string
	trust     string
	basis     string
}

// passportByHost сверяется с хостом и всеми его родительскими доменами, поэтому одна запись покрывает
// поддомены. Основание короткое: его читает аналитик.
func passportByHost() map[string]passportRule {
	hosts := passportInstitutions()
	maps.Copy(hosts, passportMedia())
	maps.Copy(hosts, passportMarketMedia())
	return hosts
}

func passportInstitutions() map[string]passportRule {
	return map[string]passportRule{
		"nasa.gov":           {PublisherGovernment, TrustHigh, "государственное космическое агентство"},
		"clinicaltrials.gov": {PublisherRegistry, TrustHigh, "государственный реестр клинических исследований"},
		"fda.gov":            {PublisherRegulator, TrustHigh, "регулятор обращения лекарств и медизделий"},
		"ema.europa.eu":      {PublisherRegulator, TrustHigh, "регулятор обращения лекарств"},
		"europa.eu":          {PublisherGovernment, TrustHigh, "орган Европейского союза"},
		"europol.europa.eu":  {PublisherRegulator, TrustHigh, "правоохранительный орган Европейского союза"},
		"cbr.ru":             {PublisherRegulator, TrustHigh, "центральный банк"},
		"iea.org":            {PublisherInterOrg, TrustHigh, "международное энергетическое агентство"},
		"wipo.int":           {PublisherInterOrg, TrustHigh, "всемирная организация интеллектуальной собственности"},
		"who.int":            {PublisherInterOrg, TrustHigh, "всемирная организация здравоохранения"},
		"oecd.org":           {PublisherInterOrg, TrustHigh, "межправительственная организация"},
		"nih.gov":            {PublisherGovernment, TrustHigh, "государственный институт здоровья"},
		"energy.gov":         {PublisherGovernment, TrustHigh, "государственное ведомство по энергетике"},
		// Документ в EDGAR пишет сама компания, а не регулятор: отвечает по закону, но сторона заинтересованная.
		"sec.gov": {PublisherVendor, TrustMedium, "отчётность публичной компании, поданная регулятору"},

		"uspto.gov":        {PublisherPatentOffice, TrustHigh, basisPatentOffice},
		"epo.org":          {PublisherPatentOffice, TrustHigh, basisPatentOffice},
		"data.epo.org":     {PublisherPatentOffice, TrustHigh, basisPatentOffice},
		"rospatent.gov.ru": {PublisherPatentOffice, TrustHigh, basisPatentOffice},
		"fips.ru":          {PublisherPatentOffice, TrustHigh, basisPatentOffice},
		"cnipa.gov.cn":     {PublisherPatentOffice, TrustHigh, basisPatentOffice},

		"news.mit.edu": {PublisherUniversity, TrustHigh, basisUniversity},
		"mit.edu":      {PublisherUniversity, TrustHigh, basisUniversity},
		"cern.ch":      {PublisherResearch, TrustHigh, "научный центр"},
		"hse.ru":       {PublisherUniversity, TrustHigh, basisUniversity},
		"skoltech.ru":  {PublisherUniversity, TrustHigh, basisUniversity},
		"ras.ru":       {PublisherResearch, TrustHigh, "академия наук"},

		"journals.plos.org":   {PublisherJournal, TrustHigh, basisPeerJournal},
		"frontiersin.org":     {PublisherJournal, TrustHigh, basisPeerJournal},
		"quantum-journal.org": {PublisherJournal, TrustHigh, basisPeerJournal},
		"nature.com":          {PublisherJournal, TrustHigh, basisPeerJournal},
		"science.org":         {PublisherJournal, TrustHigh, basisPeerJournal},
		"pubs.rsc.org":        {PublisherJournal, TrustHigh, basisPeerJournal},
		"mdpi.com":            {PublisherJournal, TrustMedium, "научный журнал с ускоренным рецензированием"},
		"ncbi.nlm.nih.gov":    {PublisherRegistry, TrustHigh, "государственная библиотека медицинских публикаций"},
		"ebi.ac.uk":           {PublisherResearch, TrustHigh, "европейский институт биоинформатики"},

		"arxiv.org":    {PublisherPreprint, TrustMedium, basisPreprint},
		"biorxiv.org":  {PublisherPreprint, TrustMedium, basisPreprint},
		"openalex.org": {PublisherAggregator, TrustMedium, basisBibliographic},
		"crossref.org": {PublisherAggregator, TrustMedium, basisBibliographic},
		"doi.org":      {PublisherUnknown, TrustMedium, "библиографическая запись; тип издателя не подтверждён"},

		"cncf.io":         {PublisherAssociation, TrustHigh, basisAssociation},
		"ietf.org":        {PublisherAssociation, TrustHigh, basisStandards},
		"w3.org":          {PublisherAssociation, TrustHigh, basisStandards},
		"iso.org":         {PublisherAssociation, TrustHigh, basisStandards},
		"owasp.org":       {PublisherAssociation, TrustHigh, basisAssociation},
		"genai.owasp.org": {PublisherAssociation, TrustHigh, basisAssociation},

		"research.google":      {PublisherVendor, TrustMedium, basisVendorResearch},
		"deepmind.google":      {PublisherVendor, TrustMedium, basisVendorResearch},
		"developer.nvidia.com": {PublisherVendor, TrustMedium, "технический блог компании-разработчика"},
		"huggingface.co":       {PublisherVendor, TrustMedium, "блог и площадка публикации моделей"},
		"microsoft.com":        {PublisherVendor, TrustMedium, basisVendorOfficial},
		"openai.com":           {PublisherVendor, TrustMedium, basisVendorOfficial},
		"anthropic.com":        {PublisherVendor, TrustMedium, basisVendorOfficial},
		"sciml.ai":             {PublisherVendor, TrustMedium, "материал проекта-разработчика"},
		"research.ibm.com":     {PublisherVendor, TrustMedium, basisVendorResearch},
		"nvidia.com":           {PublisherVendor, TrustMedium, basisVendorOfficial},
		"ycombinator.com":      {PublisherVendor, TrustMedium, "официальный материал венчурного акселератора"},
		// Пресс-центры и продуктовые блоги — пресс-релизы по сути, поэтому пониженное доверие у всего домена,
		// а не только у материалов с «объявляет» в заголовке.
		"news.samsung.com":         {PublisherVendor, TrustLowered, basisVendorNewsroom},
		"cloudblog.withgoogle.com": {PublisherVendor, TrustLowered, basisVendorNewsroom},
		"blogs.microsoft.com":      {PublisherVendor, TrustLowered, basisVendorNewsroom},
		"news.microsoft.com":       {PublisherVendor, TrustLowered, basisVendorNewsroom},
		"blog.cloudflare.com":      {PublisherVendor, TrustLowered, basisVendorNewsroom},
		"blog.google":              {PublisherVendor, TrustLowered, basisVendorNewsroom},
		"medrxiv.org":              {PublisherPreprint, TrustMedium, basisPreprint},
	}
}

func passportMedia() map[string]passportRule {
	return map[string]passportRule{
		"cnews.ru":         {PublisherMedia, TrustMedium, basisTradeMedia},
		"tadviser.ru":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"nplus1.ru":        {PublisherMedia, TrustMedium, basisPopularScience},
		"techcrunch.com":   {PublisherMedia, TrustMedium, basisIndustryMedia},
		"siliconangle.com": {PublisherMedia, TrustMedium, basisIndustryMedia},
		"eetimes.com":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"theregister.com":  {PublisherMedia, TrustMedium, basisIndustryMedia},
		"reuters.com":      {PublisherMedia, TrustMedium, basisNewsAgency},
		// Эталон организаторов почти целиком из таких изданий: без записи здесь большинство подтверждений
		// получило бы пониженное доверие неизвестного издателя. Жанр материала всё равно может его понизить.
		"technologyreview.com":       {PublisherMedia, TrustMedium, "издание технологического института"},
		"spectrum.ieee.org":          {PublisherMedia, TrustMedium, "издание профессиональной инженерной ассоциации"},
		"datacenterdynamics.com":     {PublisherMedia, TrustMedium, basisTradeMedia},
		"fierceelectronics.com":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"fiercesensors.com":          {PublisherMedia, TrustMedium, basisTradeMedia},
		"electronicsweekly.com":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"tomshardware.com":           {PublisherMedia, TrustMedium, basisTradeMedia},
		"thenewstack.io":             {PublisherMedia, TrustMedium, basisTradeMedia},
		"infoq.com":                  {PublisherMedia, TrustMedium, basisTradeMedia},
		"pymnts.com":                 {PublisherMedia, TrustMedium, basisTradeMedia},
		"bleepingcomputer.com":       {PublisherMedia, TrustMedium, basisTradeMedia},
		"thesaasnews.com":            {PublisherMedia, TrustMedium, basisTradeMedia},
		"siliconrepublic.com":        {PublisherMedia, TrustMedium, basisIndustryMedia},
		"asia.nikkei.com":            {PublisherMedia, TrustMedium, "деловое информационное агентство"},
		"sifted.eu":                  {PublisherMedia, TrustMedium, basisVentureMedia},
		"crunchbase.com":             {PublisherMedia, TrustMedium, basisVentureMedia},
		"pehub.com":                  {PublisherMedia, TrustMedium, "издание о сделках частного капитала"},
		"bloomberg.com":              {PublisherMedia, TrustMedium, "деловое информационное агентство"},
		"cnbc.com":                   {PublisherMedia, TrustMedium, basisBusinessMedia},
		"fintechnews.ch":             {PublisherMedia, TrustMedium, basisTradeMedia},
		"electrek.co":                {PublisherMedia, TrustMedium, basisTradeMedia},
		"spacenews.com":              {PublisherMedia, TrustMedium, basisTradeMedia},
		"nasaspaceflight.com":        {PublisherMedia, TrustMedium, basisTradeMedia},
		"insideclimatenews.org":      {PublisherMedia, TrustMedium, basisIndustryMedia},
		"statnews.com":               {PublisherMedia, TrustMedium, basisIndustryMedia},
		"fiercebiotech.com":          {PublisherMedia, TrustMedium, basisTradeMedia},
		"therobotreport.com":         {PublisherMedia, TrustMedium, basisTradeMedia},
		"agfundernews.com":           {PublisherMedia, TrustMedium, basisTradeMedia},
		"freightwaves.com":           {PublisherMedia, TrustMedium, basisTradeMedia},
		"insideevs.com":              {PublisherMedia, TrustMedium, basisTradeMedia},
		"3dprintingindustry.com":     {PublisherMedia, TrustMedium, basisTradeMedia},
		"the-decoder.com":            {PublisherMedia, TrustMedium, basisIndustryMedia},
		"interestingengineering.com": {PublisherMedia, TrustMedium, basisIndustryMedia},
		"phys.org":                   {PublisherAggregator, TrustMedium, basisScienceWire},
		"techxplore.com":             {PublisherAggregator, TrustMedium, basisScienceWire},
		"pulse2.com":                 {PublisherMedia, TrustLowered, "новости сделок, во многом по пресс-релизам"},
		"techport.nasa.gov":          {PublisherGovernment, TrustHigh, "реестр проектов космического агентства"},
		"raw.githubusercontent.com":  {PublisherAssociation, TrustMedium, "каталог отраслевого фонда"},

		"interfax.ru":      {PublisherMedia, TrustMedium, basisNewsAgency},
		"3dnews.ru":        {PublisherMedia, TrustMedium, basisTradeMedia},
		"comnews.ru":       {PublisherMedia, TrustMedium, basisTradeMedia},
		"rspectr.com":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"it-world.ru":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"iemag.ru":         {PublisherMedia, TrustMedium, basisTradeMedia},
		"hightech.fm":      {PublisherMedia, TrustMedium, basisPopularScience},
		"naked-science.ru": {PublisherMedia, TrustMedium, basisPopularScience},
		"tass.ru":          {PublisherMedia, TrustMedium, basisNewsAgency},
		"vedomosti.ru":     {PublisherMedia, TrustMedium, basisBusinessMedia},
		"frankmedia.ru":    {PublisherMedia, TrustMedium, "деловое медиа о финансовом рынке"},
		"incrussia.ru":     {PublisherMedia, TrustMedium, basisBusinessMedia},

		"news.ycombinator.com": {PublisherAggregator, TrustLowered, "агрегатор пользовательских ссылок"},
		"lobste.rs":            {PublisherAggregator, TrustLowered, "агрегатор пользовательских ссылок"},
		"lesswrong.com":        {PublisherBlog, TrustLowered, basisCommunityForum},
		"github.com":           {PublisherCodeHost, TrustLowered, "самопубликация в репозитории без рецензирования"},
		"pypi.org":             {PublisherCodeHost, TrustLowered, basisPackageRegistry},
		"npmjs.com":            {PublisherCodeHost, TrustLowered, basisPackageRegistry},
		"habr.com":             {PublisherBlog, TrustLowered, "площадка пользовательских публикаций"},
		"rb.ru":                {PublisherMedia, TrustLowered, "новости стартапов, во многом по пресс-релизам"},
		"vc.ru":                {PublisherBlog, TrustLowered, "площадка пользовательских и корпоративных публикаций"},
		"producthunt.com":      {PublisherAggregator, TrustLowered, "витрина запусков, которые публикуют сами авторы"},
		"medium.com":           {PublisherBlog, TrustLowered, basisBlogPlatform},
		"substack.com":         {PublisherBlog, TrustLowered, basisBlogPlatform},
		"linkedin.com":         {PublisherSocial, TrustLowered, basisSocial},
		"x.com":                {PublisherSocial, TrustLowered, basisSocial},
		"reddit.com":           {PublisherSocial, TrustLowered, basisSocial},
		"prnewswire.com":       {PublisherMedia, TrustLowered, basisPressWire},
		"businesswire.com":     {PublisherMedia, TrustLowered, basisPressWire},
		"globenewswire.com":    {PublisherMedia, TrustLowered, basisPressWire},
	}
}

// passportMarketMedia — рыночные ленты под эталон организаторов: 87 % его ссылок — медиа, сайты компаний
// и пресс-релизы.
func passportMarketMedia() map[string]passportRule {
	return map[string]passportRule{
		"theaiinsider.tech":             {PublisherMedia, TrustMedium, basisIndustryMedia},
		"geekwire.com":                  {PublisherMedia, TrustMedium, basisIndustryMedia},
		"pandaily.com":                  {PublisherMedia, TrustMedium, basisIndustryMedia},
		"venturebeat.com":               {PublisherMedia, TrustMedium, basisIndustryMedia},
		"techradar.com":                 {PublisherMedia, TrustMedium, basisIndustryMedia},
		"zdnet.com":                     {PublisherMedia, TrustMedium, basisIndustryMedia},
		"arstechnica.com":               {PublisherMedia, TrustMedium, basisIndustryMedia},
		"runtimewire.com":               {PublisherMedia, TrustMedium, basisIndustryMedia},
		"ixbt.com":                      {PublisherMedia, TrustMedium, basisIndustryMedia},
		"thequantuminsider.com":         {PublisherMedia, TrustMedium, basisTradeMedia},
		"servethehome.com":              {PublisherMedia, TrustMedium, basisTradeMedia},
		"computerweekly.com":            {PublisherMedia, TrustMedium, basisTradeMedia},
		"roboticsandautomationnews.com": {PublisherMedia, TrustMedium, basisTradeMedia},
		"semiconductor-digest.com":      {PublisherMedia, TrustMedium, basisTradeMedia},
		"eenewseurope.com":              {PublisherMedia, TrustMedium, basisTradeMedia},
		"iot-now.com":                   {PublisherMedia, TrustMedium, basisTradeMedia},
		"rcrwireless.com":               {PublisherMedia, TrustMedium, basisTradeMedia},
		"edgeir.com":                    {PublisherMedia, TrustMedium, basisTradeMedia},
		"fiercehealthcare.com":          {PublisherMedia, TrustMedium, basisTradeMedia},
		"securityweek.com":              {PublisherMedia, TrustMedium, basisTradeMedia},
		"thehackernews.com":             {PublisherMedia, TrustMedium, basisTradeMedia},
		"infosecurity-magazine.com":     {PublisherMedia, TrustMedium, basisTradeMedia},
		"cyberscoop.com":                {PublisherMedia, TrustMedium, basisTradeMedia},
		"bankinfosecurity.com":          {PublisherMedia, TrustMedium, basisTradeMedia},
		"securitylab.ru":                {PublisherMedia, TrustMedium, basisTradeMedia},
		"anti-malware.ru":               {PublisherMedia, TrustMedium, basisTradeMedia},
		"itweek.ru":                     {PublisherMedia, TrustMedium, basisTradeMedia},
		"theblock.co":                   {PublisherMedia, TrustMedium, basisTradeMedia},
		"coindesk.com":                  {PublisherMedia, TrustMedium, basisTradeMedia},
		"crowdfundinsider.com":          {PublisherMedia, TrustMedium, basisTradeMedia},
		"fintech.global":                {PublisherMedia, TrustMedium, basisTradeMedia},
		"banki.ru":                      {PublisherMedia, TrustMedium, "финансовый портал"},
		"fortune.com":                   {PublisherMedia, TrustMedium, basisBusinessMedia},
		"theinformation.com":            {PublisherMedia, TrustMedium, basisBusinessMedia},
		"ft.com":                        {PublisherMedia, TrustMedium, basisBusinessMedia},
		"wsj.com":                       {PublisherMedia, TrustMedium, basisBusinessMedia},
		"economist.com":                 {PublisherMedia, TrustMedium, basisBusinessMedia},

		"techstartups.com":    {PublisherMedia, TrustLowered, "новости стартапов, во многом по пресс-релизам"},
		"semiwiki.com":        {PublisherBlog, TrustLowered, "отраслевое сообщество: авторские и спонсорские публикации"},
		"edge-ai-vision.com":  {PublisherAssociation, TrustLowered, "сайт отраслевого альянса: материалы компаний-участников"},
		"alignmentforum.org":  {PublisherBlog, TrustLowered, basisCommunityForum},
		"krebsonsecurity.com": {PublisherBlog, TrustLowered, basisAuthorNewsletter},
		"aisnakeoil.com":      {PublisherBlog, TrustLowered, basisAuthorNewsletter},
		"normaltech.ai":       {PublisherBlog, TrustLowered, basisAuthorNewsletter},
		"semianalysis.com":    {PublisherBlog, TrustLowered, basisAuthorNewsletter},
		"interconnects.ai":    {PublisherBlog, TrustLowered, basisAuthorNewsletter},
	}
}

func passportBySuffix() []struct {
	suffix string
	rule   passportRule
} {
	return []struct {
		suffix string
		rule   passportRule
	}{
		{".gov", passportRule{PublisherGovernment, TrustHigh, basisGovDomain}},
		{".gov.ru", passportRule{PublisherGovernment, TrustHigh, basisGovDomain}},
		{".gov.uk", passportRule{PublisherGovernment, TrustHigh, basisGovDomain}},
		{".gov.cn", passportRule{PublisherGovernment, TrustHigh, basisGovDomain}},
		{".edu", passportRule{PublisherUniversity, TrustHigh, basisEduDomain}},
		{".ac.uk", passportRule{PublisherUniversity, TrustHigh, basisEduDomain}},
		{".edu.cn", passportRule{PublisherUniversity, TrustHigh, basisEduDomain}},
		{".int", passportRule{PublisherInterOrg, TrustHigh, "домен международной организации"}},
	}
}

// ClassifySource: адрес вне реестра намеренно получает пониженное доверие — неклассифицированный
// источник не должен молча считаться доверенным.
func ClassifySource(rawURL string) SourcePassport {
	host := passportHost(rawURL)
	if host == "" {
		return SourcePassport{PublisherUnknown, TrustLowered, "адрес источника не разобран"}
	}
	// Работы окна препринтов ссылаются на DOI: издателя выдаёт префикс сервера, а не общий doi.org.
	if host == "doi.org" && livePreprintDOI(rawURL) {
		return SourcePassport{PublisherPreprint, TrustMedium, basisPreprint}
	}
	hosts := passportByHost()
	for domain := host; domain != ""; domain = parentDomain(domain) {
		if rule, ok := hosts[domain]; ok {
			return SourcePassport{rule.publisher, rule.trust, rule.basis}
		}
	}
	for _, entry := range passportBySuffix() {
		if strings.HasSuffix(host, entry.suffix) {
			return SourcePassport{entry.rule.publisher, entry.rule.trust, entry.rule.basis}
		}
	}
	return SourcePassport{PublisherUnknown, TrustLowered, "домен не внесён в реестр доверия"}
}

func passportHost(rawURL string) string {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "//") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
}

func parentDomain(domain string) string {
	if _, rest, found := strings.Cut(domain, "."); found && strings.Contains(rest, ".") {
		return rest
	}
	return ""
}

func stampEventPassports(events []StudyEvent) {
	for i := range events {
		passport := ClassifySource(events[i].URL)
		events[i].PublisherType = passport.Publisher
		events[i].Trust = passport.Trust
		events[i].TrustBasis = passport.TrustBasis
	}
}

func stampPaperPassports(papers []StudyPaper, metadata map[string]LivePaperMetadata) {
	for _, paper := range papers {
		meta := metadata[paper.ID]
		address := cmp.Or(paper.URL, meta.SourceURL)
		passport, genre := liveApplyGenre(ClassifySource(address), address, paper.Title)
		meta.PublisherType = passport.Publisher
		meta.Genre = cmp.Or(meta.Genre, genre)
		meta.Trust = passport.Trust
		meta.TrustBasis = passport.TrustBasis
		if liveSourceOf(paper.ID) == livePubMedSource {
			// Индексация медицинской библиотекой подтверждает происхождение записи, а не качество результата.
			meta.PublisherType, meta.Trust = PublisherAggregator, TrustMedium
			meta.TrustBasis = "публикация индексируется в PubMed; качество исследования отдельно не проверено"
		}
		meta.Language, meta.LanguageBasis = livePaperLanguage(paper, meta)
		metadata[paper.ID] = meta
	}
}

// liveTextScript называет язык, только когда алфавит не оставляет выбора: кириллица — русский, иероглифы —
// китайский. Латиница намеренно не решается: она общая для английского, немецкого и французского.
func liveTextScript(text string) string {
	cyrillic, han, latin := 0, 0, 0
	for _, r := range text {
		switch {
		case r >= 0x0400 && r <= 0x04FF:
			cyrillic++
		case r >= 0x4E00 && r <= 0x9FFF:
			han++
		case r|0x20 >= 'a' && r|0x20 <= 'z':
			latin++
		}
	}
	switch {
	case han*4 >= cyrillic+latin+han:
		return "zh"
	case cyrillic > latin:
		return "ru"
	}
	return ""
}

const liveLanguageUnknown = "unknown"

// livePaperLanguage возвращает язык вместе с основанием: ответ не показывает язык, не сказав, откуда он.
func livePaperLanguage(paper StudyPaper, meta LivePaperMetadata) (code, basis string) {
	if meta.Language != "" {
		return meta.Language, cmp.Or(meta.LanguageBasis, "язык пришёл от источника")
	}
	if script := liveTextScript(paper.Title + " " + meta.Abstract); script != "" {
		return script, "определён по преобладающему алфавиту заголовка и аннотации"
	}
	host := passportHost(cmp.Or(paper.URL, meta.SourceURL))
	for domain := host; domain != ""; domain = parentDomain(domain) {
		switch domain {
		case "arxiv.org", "europepmc.org", "ncbi.nlm.nih.gov":
			return "en", "площадка принимает метаданные только на английском"
		}
	}
	// Научная публикация латиницей почти всегда англоязычна; основание честно называет это выводом.
	switch ClassifySource(cmp.Or(paper.URL, meta.SourceURL)).Publisher {
	case PublisherJournal, PublisherPreprint, PublisherUniversity, PublisherResearch, PublisherAggregator:
		if liveLatin(paper.Title + " " + meta.Abstract) {
			return "en", "язык не заявлен источником; латинский алфавит в научной публикации"
		}
	}
	return "", ""
}

func liveLatin(text string) bool {
	latin := 0
	for _, r := range text {
		if r|0x20 >= 'a' && r|0x20 <= 'z' {
			latin++
		}
	}
	return latin >= 12
}

// LivePaperPassport — срез метаданных работы для ответа; остальное в записи — происхождение, страница его не показывает.
type LivePaperPassport struct {
	Language        string `json:"language,omitempty"`
	LanguageBasis   string `json:"language_basis,omitempty"`
	PublisherType   string `json:"publisher_type,omitempty"`
	Genre           string `json:"genre,omitempty"`
	SourceName      string `json:"source_name,omitempty"`
	DiscoverySource string `json:"discovery_source,omitempty"`
	Trust           string `json:"trust,omitempty"`
	TrustBasis      string `json:"trust_basis,omitempty"`
	// Digest — сгенерированный русский пересказ; имя модели рядом обязательно: постановка требует помечать такой текст.
	Digest      string `json:"digest,omitempty"`
	DigestModel string `json:"digest_model,omitempty"`
}

// livePaperPassports пропускает работы без измеренных признаков: пустая карточка утверждала бы проверку,
// которой не было.
func livePaperPassports(papers []StudyPaper, metadata map[string]LivePaperMetadata) map[string]LivePaperPassport {
	passports := map[string]LivePaperPassport{}
	for _, paper := range papers {
		meta, ok := metadata[paper.ID]
		if !ok {
			continue
		}
		card := LivePaperPassport{Language: meta.Language, LanguageBasis: meta.LanguageBasis,
			PublisherType: meta.PublisherType, Genre: meta.Genre, Trust: meta.Trust, TrustBasis: meta.TrustBasis,
			Digest: meta.Digest, DigestModel: meta.DigestModel}
		// У агрегатора поле категории — название издания, а у препринта — тематический код, не издатель.
		switch source := liveSourceOf(paper.ID); source {
		case "openalex", livePubMedSource:
			card.SourceName, card.DiscoverySource = meta.Category, source
		case "rec":
			if name := cmp.Or(liveJournalProvider(paper.ID), liveJournalProvider(meta.Category)); name != "" {
				card.SourceName, card.DiscoverySource = name, liveSourceOf(strings.TrimPrefix(paper.ID, liveRecordPrefix))
			}
		case "arxiv":
			card.SourceName, card.DiscoverySource = "arXiv", source
		default:
			if name := livePreprintServerName(source); name != "" {
				card.SourceName, card.DiscoverySource = name, source
			}
		}
		if card == (LivePaperPassport{}) {
			continue
		}
		// «Не определён» пишется явно: omitempty иначе спрятал бы отсутствие измерения.
		if card.Language == "" || card.Language == liveLanguageUnknown {
			card.Language = liveLanguageUnknown
			card.LanguageBasis = cmp.Or(card.LanguageBasis, "язык не заявлен источником; по доступным метаданным не определён")
		}
		card.Genre = cmp.Or(card.Genre, GenreUnknown)
		passports[paper.ID] = card
	}
	if len(passports) == 0 {
		return nil
	}
	return passports
}

// Жанр ограничивает доверие сверху независимо от издателя: пресс-релиз на сайте ведомства остаётся
// пресс-релизом. Определяется по адресу и названию — они есть у записи всегда.
const (
	GenreAnnouncement = "press_release"
	GenreBlog         = "blog_post"
	GenreOpinion      = "opinion"
	GenreNews         = "news"
	GenreRecord       = "official_record"
	GenrePeerReviewed = "peer_reviewed"
	GenreUnknown      = "unknown"
)

// SourceGenre — жанр материала и потолок доверия для него: анонс доказывает, что объявили, но не что работает.
type SourceGenre struct {
	Genre   string
	Ceiling string
	Basis   string
}

type liveGenreRule struct {
	genre, ceiling, basis string
	paths, words          []string
}

// liveGenreRules: побеждает первое совпадение, поэтому узкие жанры идут первыми.
func liveGenreRules() []liveGenreRule {
	return []liveGenreRule{
		{GenreAnnouncement, TrustLowered, "пресс-релиз: подтверждает объявление, но не проверку заявленного",
			[]string{"press-release", "press_release", "pressrelease", "/press/", "/pr/", "news-release", "newsroom",
				"/media-release", "/presse/", "/press-center", "/press-centre"},
			[]string{"announces", "announcing", "announced", "unveils", "unveiled", "launches", "introduces",
				"introducing", "partners with", "объявляет", "анонсирует", "запускает", "представляет"}},
		{GenreBlog, TrustLowered, "запись в блоге: авторский текст без редакционной проверки",
			[]string{"/blog/", "/blogs/", "medium.com", "substack.com", "/posts/"}, nil},
		{GenreOpinion, TrustLowered, "колонка или мнение: позиция автора, а не проверенный факт",
			[]string{"/opinion/", "/column/", "/editorial/", "/viewpoint/", "/perspective/"}, nil},
		{GenreRecord, TrustHigh, "официальная запись ведомства или реестра",
			[]string{"/patent/", "/patents/", "/register/", "/registry/", "/filing/", "/docket/"}, nil},
		{GenreNews, TrustMedium, "новостная заметка: сообщает о событии, независимой проверки не содержит",
			[]string{"/news/", "/article/", "/story/", "/novosti/"}, nil},
	}
}

func ClassifyGenre(rawURL, title string) SourceGenre {
	haystack := strings.ToLower(rawURL)
	lowered := strings.ToLower(title)
	for _, rule := range liveGenreRules() {
		for _, path := range rule.paths {
			if strings.Contains(haystack, path) {
				return SourceGenre{rule.genre, rule.ceiling, rule.basis}
			}
		}
		for _, word := range rule.words {
			if strings.Contains(lowered, word) {
				return SourceGenre{rule.genre, rule.ceiling, rule.basis}
			}
		}
	}
	return SourceGenre{GenreUnknown, "", ""}
}

func liveTrustOrder(trust string) int {
	switch trust {
	case TrustHigh:
		return 3
	case TrustMedium:
		return 2
	case TrustLowered:
		return 1
	}
	return 0
}

// liveApplyGenre только понижает доверие: официальная запись на неизвестном хосте остаётся неизвестным хостом.
func liveApplyGenre(passport SourcePassport, rawURL, title string) (SourcePassport, string) {
	genre := ClassifyGenre(rawURL, title)
	if genre.Ceiling == "" || liveTrustOrder(genre.Ceiling) >= liveTrustOrder(passport.Trust) {
		return passport, genre.Genre
	}
	passport.Trust = genre.Ceiling
	passport.TrustBasis = genre.Basis + "; издатель: " + passport.TrustBasis
	return passport, genre.Genre
}
