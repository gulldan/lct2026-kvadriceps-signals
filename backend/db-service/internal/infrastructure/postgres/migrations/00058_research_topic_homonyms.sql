-- +goose Up
-- Создано из frontend/src/shared/lib/researchTaxonomy.json; порядок сохраняет одну категорию на запись.
-- Родительские категории получают только остаток после исследовательских подрубрик.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION research_topic(input text) RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE normalized text := ' ' || lower(coalesce(input,'')) || ' ';
BEGIN
 RETURN CASE
 WHEN normalized LIKE ANY(ARRAY[
  '%online dating%','%dating app%','%dating platform%','%romantic partner%','%matchmaking platform%','%romantic relationship%','%онлайн%знакомств%','%приложени%знакомств%','%сервис%знакомств%','%поиск%романтическ%партнер%','%поиск%романтическ%партнёр%'
 ]) THEN 'dating'
 WHEN normalized LIKE ANY(ARRAY[
  '%agricultur%','%agronom%','%crop%','%soil%','%irrigat%','%fertiliz%','%livestock%','%aquacultur%','%plant breeding%','%сельскохозяй%','%агро%','%урожай%','%почв%','%орошени%','%удобрени%','%животновод%','%аквакультур%'
 ]) THEN 'agriculture'
 WHEN normalized LIKE ANY(ARRAY[
  '%wastewater%','%water treatment%','%water purification%','%desalinat%','%drinking water%','%water supply%','%sewage%','%water infrastructure%','%water resource%','%очистк%вод%','%водоочист%','%опреснени%','%питьев%вод%','%водоснабж%','%сточн%вод%','%водн%ресурс%'
 ]) THEN 'water'
 WHEN normalized LIKE ANY(ARRAY[
  '%climate%','%environment%','%ecolog%','%biodiversity%','%carbon capture%','%carbon sequestration%','%carbon emission%','%pollution%','%recycl%','%ecosystem%','%климат%','%эколог%','%биоразнообраз%','%декарбониз%','%загрязнен%','%переработк%отход%'
 ]) THEN 'environment'
 WHEN normalized LIKE ANY(ARRAY[
  '%biotech%','%synthetic biology%','%genom%','%gene edit%','%crispr%','%proteom%','%microbiom%','%ferment%','%enzyme%','%biomolecul%','%биотех%','%синтетическ%биолог%','%геном%','%редактирован%ген%','%фермент%','%микробиом%'
 ]) THEN 'biotechnology'
 WHEN normalized LIKE ANY(ARRAY[
  '%education%','%edtech%','%learning outcome%','%student achievement%','%tutoring%','%pedagog%','%обучени%школьник%','%успеваемост%','%педагог%','%репетитор%','%образовательн%','%систем%образован%','%школьн%образован%','%высш%образован%','%дошкольн%образован%','%дистанционн%образован%','%учебн%','%student outcomes%','%teacher education%','%teacher professional development%','%school achievement%','%school curriculum%','%school funding%','%school attendance%','%school leadership%','%classroom teaching%','%средн%образован%','%доступ%образован%'
 ]) THEN 'education'
 WHEN normalized LIKE ANY(ARRAY[
  '%clinical%','%patient%','%cancer%','%oncolog%','%therap%','%diagnos%','%vaccine%','%surgery%','%surgical%','%disease%','%pharmac%','%drug discovery%','%health care%','%healthcare%','%medical%','%medicine%','%dental%','%pediatric%','%paediatric%','%медицин%','%клинич%','%пациент%','%онкол%','%диагност%','%терап%','%вакцин%','%заболев%','% лечени%','%педиатр%','%стоматолог%'
 ]) THEN 'medicine'
 WHEN normalized LIKE ANY(ARRAY[
  '%food%','%nutrition%','%cultivated meat%','%alternative protein%','%food packaging%','%пищев%','%продовольств%','%культивируем%мяс%','%альтернативн%бел%','%нутрици%','%рацион%питани%'
 ]) THEN 'food'
 WHEN normalized LIKE ANY(ARRAY[
  '%energy%','%battery%','%batteries%','%solar cell%','%photovoltaic%','%hydrogen%','%fuel cell%','%electrolys%','%electrolyz%','%power grid%','%wind turbine%','%nuclear reactor%','%энерг%','%аккумулятор%','%батаре%','%водород%','%топлив%','%электролиз%','%солнечн%','%электросет%','%электропитан%'
 ]) THEN 'energy'
 WHEN normalized LIKE ANY(ARRAY[
  '%spacecraft%','%satellite%','%astronom%','%astrophys%','%cosmolog%','%exoplanet%','%galaxi%','%galaxy%','%telescope%','%космич%','%спутник%','%астроном%','%астрофиз%','%космолог%','%галактик%','%телескоп%'
 ]) THEN 'space'
 WHEN normalized LIKE ANY(ARRAY[
  '%fashion%','%textile%','%apparel%','%garment%','%footwear%','%clothing%','%wearable fabric%','% мода %','% моды %','%модная%','%текстил%','%одежд%','%обув%','%швейн%'
 ]) THEN 'fashion'
 WHEN normalized LIKE ANY(ARRAY[
  '%fintech%','%digital payment%','%mobile payment%','%payment system%','%digital banking%','%open banking%','%credit scoring%','%insurance technolog%','%insurtech%','%blockchain%','%decentralized finance%','%финтех%','%цифров%платеж%','%цифров%платёж%','%платежн%систем%','%платёжн%систем%','%цифров%банк%','%кредитн%скоринг%','%страхов%технолог%','%блокчейн%'
 ]) THEN 'fintech'
 WHEN normalized LIKE ANY(ARRAY[
  '%video game%','%videogame%','%gaming%','%game engine%','%cloud gaming%','%board game%','%видеоигр%','%гейминг%','%игров%движ%','%компьютерн%игр%','%настольн%игр%'
 ]) THEN 'gaming'
 WHEN normalized LIKE ANY(ARRAY[
  '%music%','%musical instrument%','%audio synthesis%','%музык%','%синтез%аудио%'
 ]) THEN 'music'
 WHEN normalized LIKE ANY(ARRAY[
  '%marketing%','%advertising%','%adtech%','%martech%','%customer engagement%','%customer acquisition%','%advertisement%','%маркетинг%','%реклам%','%привлечени%клиент%','%вовлечени%клиент%'
 ]) THEN 'marketing'
 WHEN normalized LIKE ANY(ARRAY[
  '%legal tech%','%legaltech%','%legal service%','%legal research%','%contract analysis%','%judicial%','%court decision%','%access to justice%','%юридическ%','%правов%технолог%','%судебн%','%анализ%договор%','%доступ%правосуд%'
 ]) THEN 'legal'
 WHEN normalized LIKE ANY(ARRAY[
  '%recruitment%','%recruiting%','%hiring%','%talent acquisition%','%human resource%','%workforce management%','%employee retention%','%подбор%персонал%','%рекрутинг%','%найм%сотрудник%','%кадров%','%управлени%персонал%','%удержани%сотрудник%'
 ]) THEN 'jobs_recruitment'
 WHEN normalized LIKE ANY(ARRAY[
  '%smart home%','%home automation%','%domotics%','%household appliance%','%smart garden%','%умн%дом%','%домашн%автоматизац%','%бытов%техник%','%домашн%устройств%'
 ]) THEN 'home_living'
 WHEN normalized LIKE ANY(ARRAY[
  '%event management%','%event platform%','%conference management%','%event ticketing%','%event technology%','%event tech%','%управлени%мероприят%','%платформ%мероприят%','%организац%конференц%','%продаж%билет%','%технолог%мероприят%'
 ]) THEN 'event_tech'
 WHEN normalized LIKE ANY(ARRAY[
  '%wellness%','%beauty tech%','%skincare%','%skin care%','%cosmetic%','%personal care%','%mindfulness%','%wellbeing%','%well-being%','%косметик%','%косметолог%','%уход%кож%','%технолог%красот%','%благополуч%','%осознанност%'
 ]) THEN 'wellness_beauty'
 WHEN normalized LIKE ANY(ARRAY[
  '%childcare%','%child care%','%parenting app%','%baby product%','%infant product%','%educational toy%','%child safety device%','%детск%товар%','%детск%сервис%','%уход%ребенк%','%уход%ребёнк%','%развивающ%игруш%','%приложени%родител%'
 ]) THEN 'kids'
 WHEN normalized LIKE ANY(ARRAY[
  '%tourism%','%tourist%','%travel booking%','%travel platform%','%hospitality%','%hotel booking%','%туризм%','%турист%','%бронирован%отел%','%платформ%путешеств%','%гостиниц%'
 ]) THEN 'travel'
 WHEN normalized LIKE ANY(ARRAY[
  '% sports%','%athlete%','%athletic%','%sports performance%','%fitness tracking%','%спортив%','%спортсмен%','%фитнес%трек%','% спорта%'
 ]) THEN 'sports'
 WHEN normalized LIKE ANY(ARRAY[
  '%vehicle%','%traffic%','%logistics%','%supply chain%','%autonomous driv%','%railway%','%aviation%','%автомобил%','%логистик%','%цепочк%постав%','%автономн%вожден%','%авиаци%','%железнодорож%','%transportation%','%public transport%','%urban transport%','%transport infrastructure%','%transport policy%','%freight transport%','%passenger transport%','%road transport%','%maritime transport%','%грузоперевоз%','%общественн%транспорт%','%городск%транспорт%','%транспортн%инфраструктур%','%пассажирск%перевоз%','%автотранспорт%'
 ]) THEN 'transport'
 WHEN normalized LIKE ANY(ARRAY[
  '%cloud computing%','%cloud infrastructure%','%cloud storage%','%data center%','%data centre%','%web hosting%','%serverless%','%edge computing%','%distributed storage%','%облачн%инфраструктур%','%облачн%вычислен%','%облачн%хранени%','%центр%обработк%данных%','%хостинг%','%периферийн%вычислен%','%распределенн%хранени%','%распределённ%хранени%'
 ]) THEN 'hosting'
 WHEN normalized LIKE ANY(ARRAY[
  '%real estate%','%proptech%','%property valuation%','%property management%','%housing market%','%building information model%','%недвижимост%','%проптех%','%оценк%жиль%','%управлени%здания%','%рынок%жиль%'
 ]) THEN 'real_estate'
 WHEN normalized LIKE ANY(ARRAY[
  '%robot%','%cobot%','%manipulator%','%unmanned aerial%','%humanoid%','%робот%','%кобот%','%манипулятор%','%беспилотн%летательн%','%гуманоид%'
 ]) THEN 'robotics'
 WHEN normalized LIKE ANY(ARRAY[
  '%media production%','%news media%','%journalism%','%video streaming%','%film production%','%digital content%','%content creation%','%social media%','%журналистик%','%медиапроизводств%','%цифров%контент%','%производств%видео%','%социальн%сет%','%видеостриминг%'
 ]) THEN 'media'
 WHEN normalized LIKE ANY(ARRAY[
  '%telecom%','%wireless communication%','%mobile communication%','%cellular network%','% 5g %','% 6g %','%radio access network%','%optical communication%','%телеком%','%беспроводн%связ%','%мобильн%связ%','%сотов%сет%','%радиодоступ%','%оптическ%связ%'
 ]) THEN 'telecom'
 WHEN normalized LIKE ANY(ARRAY[
  '%semiconductor%','%transistor%','%integrated circuit%','%microchip%','%microelectronic%','%chip design%','%chiplet%','%photonic chip%','%полупроводник%','%транзистор%','%интегральн%схем%','%микроэлектрон%','%микросхем%','%проектирован%чип%'
 ]) THEN 'semiconductors'
 WHEN normalized LIKE ANY(ARRAY[
  '%consumer electronic%','%smartphone%','%smartwatch%','%wearable device%','%smart glasses%','%headphone%','%потребительск%электроник%','%смартфон%','%умн%час%','%носим%устройств%','%умн%очк%','%наушник%'
 ]) THEN 'consumer_electronics'
 WHEN normalized LIKE ANY(ARRAY[
  '%cybersecur%','%cyber secur%','%information security%','%data security%','%network security%','%computer security%','%intrusion detection%','%malware%','%ransomware%','%cryptograph%','%access control%','%physical security%','%threat detection%','%кибербезопас%','%информационн%безопас%','%защит%данных%','%сетев%безопас%','%обнаружени%вторжен%','%вредоносн%программ%','%криптограф%','%контрол%доступ%'
 ]) THEN 'security'
 WHEN normalized LIKE ANY(ARRAY[
  '%material%','%metallurg%','%catalys%','%polymer%','%composite%','%ceramic%','%alloy%','%chemical%','%chemistry%','%nanostructur%','%nanoparticl%','% cement%','%concrete%','%материал%','%металлург%','%катализ%','%полимер%','%композит%','%керамик%','%сплав%','%хими%','%наночастиц%','%цемент%','%бетон%','%извлечени%меди%','%обогащени%руд%','%copper extraction%','%mineral processing%'
 ]) THEN 'materials'
 WHEN normalized LIKE ANY(ARRAY[
  '%manufactur%','%industrial%','%machining%','%3d print%','%predictive maintenance%','%fabrication%','%строительств%','%промышлен%','%производств%','%станк%','%аддитив%'
 ]) THEN 'industry'
 WHEN normalized LIKE ANY(ARRAY[
  '%computer%','%computing%','%software%','%algorithm%','%neural network%','%machine learning%','%deep learning%','%language model%','%artificial intelligence%','%information retrieval%','%database%','%retrieval augmented%','%компьютер%','%вычислен%','%программ%','%алгоритм%','%нейросет%','%машинн%обучен%','%языков%модел%','%искусственн%интеллект%','%large language%','%generative%','%генератив%','%ранжирован%','%retrieval%','%reinforcement learning%','%обучение с подкреплением%','%инференс%','%квантован%вес%','%weight quantization%'
 ]) THEN 'computing'
 WHEN normalized LIKE ANY(ARRAY[
  '%econom%','%financ%','%banking%','%monetary%','%inflation%','%social polic%','%public polic%','%demograph%','%sociolog%','%демограф%','%эконом%','%финанс%','%инфляц%','%социальн%политик%','%общественн%политик%','%социолог%'
 ]) THEN 'economics'
 WHEN normalized LIKE ANY(ARRAY[
  '%quantum%','%physics%','%mathematic%','%topolog%','%algebra%','%theorem%','%particle%','%fluid dynamic%','%radiocarbon%','%физик%','%математ%','%тополог%','%алгебр%','%теорем%','%гидродинам%','%радиоуглерод%','%квантовая%','%квантовое%','%квантовые%','%квантовый%','%квантовой%','%квантового%','%квантовым%','%квантовых%','%квантовую%','%квантово-%'
 ]) THEN 'science'
 ELSE '' END;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION research_topic(input text) RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE normalized text := ' ' || lower(coalesce(input,'')) || ' ';
BEGIN
 RETURN CASE
 WHEN normalized LIKE ANY(ARRAY[
  '%online dating%','%dating app%','%dating platform%','%romantic partner%','%matchmaking platform%','%romantic relationship%','%онлайн%знакомств%','%приложени%знакомств%','%сервис%знакомств%','%поиск%романтическ%партнер%','%поиск%романтическ%партнёр%'
 ]) THEN 'dating'
 WHEN normalized LIKE ANY(ARRAY[
  '%agricultur%','%agronom%','%crop%','%soil%','%irrigat%','%fertiliz%','%livestock%','%aquacultur%','%plant breeding%','%сельскохозяй%','%агро%','%урожай%','%почв%','%орошени%','%удобрени%','%животновод%','%аквакультур%'
 ]) THEN 'agriculture'
 WHEN normalized LIKE ANY(ARRAY[
  '%wastewater%','%water treatment%','%water purification%','%desalinat%','%drinking water%','%water supply%','%sewage%','%water infrastructure%','%water resource%','%очистк%вод%','%водоочист%','%опреснени%','%питьев%вод%','%водоснабж%','%сточн%вод%','%водн%ресурс%'
 ]) THEN 'water'
 WHEN normalized LIKE ANY(ARRAY[
  '%climate%','%environment%','%ecolog%','%biodiversity%','%carbon capture%','%carbon sequestration%','%carbon emission%','%pollution%','%recycl%','%ecosystem%','%климат%','%эколог%','%биоразнообраз%','%декарбониз%','%загрязнен%','%переработк%отход%'
 ]) THEN 'environment'
 WHEN normalized LIKE ANY(ARRAY[
  '%biotech%','%synthetic biology%','%genom%','%gene edit%','%crispr%','%proteom%','%microbiom%','%ferment%','%enzyme%','%biomolecul%','%биотех%','%синтетическ%биолог%','%геном%','%редактирован%ген%','%фермент%','%микробиом%'
 ]) THEN 'biotechnology'
 WHEN normalized LIKE ANY(ARRAY[
  '%education%','%edtech%','%learning outcome%','%student achievement%','%tutoring%','%pedagog%','%обучени%школьник%','%успеваемост%','%педагог%','%репетитор%','%образовательн%','%систем%образован%','%школьн%образован%','%высш%образован%','%дошкольн%образован%','%дистанционн%образован%','%учебн%','%student outcomes%','%teacher education%','%teacher professional development%','%school achievement%','%school curriculum%','%school funding%','%school attendance%','%school leadership%','%classroom teaching%','%средн%образован%','%доступ%образован%'
 ]) THEN 'education'
 WHEN normalized LIKE ANY(ARRAY[
  '%clinical%','%patient%','%cancer%','%oncolog%','%therap%','%diagnos%','%vaccine%','%surgery%','%surgical%','%disease%','%pharmac%','%drug discovery%','%health care%','%healthcare%','%medical%','%medicine%','%dental%','%pediatric%','%paediatric%','%медицин%','%клинич%','%пациент%','%онкол%','%диагност%','%терап%','%вакцин%','%заболев%','% лечени%','%педиатр%','%стоматолог%'
 ]) THEN 'medicine'
 WHEN normalized LIKE ANY(ARRAY[
  '%food%','%nutrition%','%cultivated meat%','%alternative protein%','%food packaging%','%пищев%','%продовольств%','%культивируем%мяс%','%альтернативн%бел%','%нутрици%','%рацион%питани%'
 ]) THEN 'food'
 WHEN normalized LIKE ANY(ARRAY[
  '%energy%','%battery%','%batteries%','%solar cell%','%photovoltaic%','%hydrogen%','%fuel cell%','%electrolys%','%electrolyz%','%power grid%','%wind turbine%','%nuclear reactor%','%энерг%','%аккумулятор%','%батаре%','%водород%','%топлив%','%электролиз%','%солнечн%','%электросет%','%электропитан%'
 ]) THEN 'energy'
 WHEN normalized LIKE ANY(ARRAY[
  '%spacecraft%','%satellite%','%astronom%','%astrophys%','%cosmolog%','%exoplanet%','%galaxi%','%galaxy%','%telescope%','%космич%','%спутник%','%астроном%','%астрофиз%','%космолог%','%галактик%','%телескоп%'
 ]) THEN 'space'
 WHEN normalized LIKE ANY(ARRAY[
  '%fashion%','%textile%','%apparel%','%garment%','%footwear%','%clothing%','%wearable fabric%','% мода %','% моды %','%модная%','%текстил%','%одежд%','%обув%','%швейн%'
 ]) THEN 'fashion'
 WHEN normalized LIKE ANY(ARRAY[
  '%fintech%','%digital payment%','%mobile payment%','%payment system%','%digital banking%','%open banking%','%credit scoring%','%insurance technolog%','%insurtech%','%blockchain%','%decentralized finance%','%финтех%','%цифров%платеж%','%цифров%платёж%','%платежн%систем%','%платёжн%систем%','%цифров%банк%','%кредитн%скоринг%','%страхов%технолог%','%блокчейн%'
 ]) THEN 'fintech'
 WHEN normalized LIKE ANY(ARRAY[
  '%video game%','%videogame%','%gaming%','%game engine%','%cloud gaming%','%board game%','%видеоигр%','%гейминг%','%игров%движ%','%компьютерн%игр%','%настольн%игр%'
 ]) THEN 'gaming'
 WHEN normalized LIKE ANY(ARRAY[
  '%music%','%musical instrument%','%audio synthesis%','%музык%','%синтез%аудио%'
 ]) THEN 'music'
 WHEN normalized LIKE ANY(ARRAY[
  '%marketing%','%advertising%','%adtech%','%martech%','%customer engagement%','%customer acquisition%','%advertisement%','%маркетинг%','%реклам%','%привлечени%клиент%','%вовлечени%клиент%'
 ]) THEN 'marketing'
 WHEN normalized LIKE ANY(ARRAY[
  '%legal tech%','%legaltech%','%legal service%','%legal research%','%contract analysis%','%judicial%','%court decision%','%access to justice%','%юридическ%','%правов%технолог%','%судебн%','%анализ%договор%','%доступ%правосуд%'
 ]) THEN 'legal'
 WHEN normalized LIKE ANY(ARRAY[
  '%recruitment%','%recruiting%','%hiring%','%talent acquisition%','%human resource%','%workforce management%','%employee retention%','%подбор%персонал%','%рекрутинг%','%найм%сотрудник%','%кадров%','%управлени%персонал%','%удержани%сотрудник%'
 ]) THEN 'jobs_recruitment'
 WHEN normalized LIKE ANY(ARRAY[
  '%smart home%','%home automation%','%domotics%','%household appliance%','%smart garden%','%умн%дом%','%домашн%автоматизац%','%бытов%техник%','%домашн%устройств%'
 ]) THEN 'home_living'
 WHEN normalized LIKE ANY(ARRAY[
  '%event management%','%event platform%','%conference management%','%event ticketing%','%event technology%','%event tech%','%управлени%мероприят%','%платформ%мероприят%','%организац%конференц%','%продаж%билет%','%технолог%мероприят%'
 ]) THEN 'event_tech'
 WHEN normalized LIKE ANY(ARRAY[
  '%wellness%','%beauty tech%','%skincare%','%skin care%','%cosmetic%','%personal care%','%mindfulness%','%wellbeing%','%well-being%','%косметик%','%косметолог%','%уход%кож%','%технолог%красот%','%благополуч%','%осознанност%'
 ]) THEN 'wellness_beauty'
 WHEN normalized LIKE ANY(ARRAY[
  '%childcare%','%child care%','%parenting app%','%baby product%','%infant product%','%educational toy%','%child safety device%','%детск%товар%','%детск%сервис%','%уход%ребенк%','%уход%ребёнк%','%развивающ%игруш%','%приложени%родител%'
 ]) THEN 'kids'
 WHEN normalized LIKE ANY(ARRAY[
  '%tourism%','%tourist%','%travel booking%','%travel platform%','%hospitality%','%hotel booking%','%туризм%','%турист%','%бронирован%отел%','%платформ%путешеств%','%гостиниц%'
 ]) THEN 'travel'
 WHEN normalized LIKE ANY(ARRAY[
  '%sports%','%athlete%','%athletic%','%sports performance%','%fitness tracking%','%спортив%','%спортсмен%','%фитнес%трек%','%спорта%'
 ]) THEN 'sports'
 WHEN normalized LIKE ANY(ARRAY[
  '%transport%','%vehicle%','%traffic%','%logistics%','%supply chain%','%autonomous driv%','%railway%','%aviation%','%транспорт%','%автомобил%','%логистик%','%цепочк%постав%','%автономн%вожден%','%авиаци%','%железнодорож%'
 ]) THEN 'transport'
 WHEN normalized LIKE ANY(ARRAY[
  '%cloud computing%','%cloud infrastructure%','%cloud storage%','%data center%','%data centre%','%web hosting%','%serverless%','%edge computing%','%distributed storage%','%облачн%инфраструктур%','%облачн%вычислен%','%облачн%хранени%','%центр%обработк%данных%','%хостинг%','%периферийн%вычислен%','%распределенн%хранени%','%распределённ%хранени%'
 ]) THEN 'hosting'
 WHEN normalized LIKE ANY(ARRAY[
  '%real estate%','%proptech%','%property valuation%','%property management%','%housing market%','%building information model%','%недвижимост%','%проптех%','%оценк%жиль%','%управлени%здания%','%рынок%жиль%'
 ]) THEN 'real_estate'
 WHEN normalized LIKE ANY(ARRAY[
  '%robot%','%cobot%','%manipulator%','%unmanned aerial%','%humanoid%','%робот%','%кобот%','%манипулятор%','%беспилотн%летательн%','%гуманоид%'
 ]) THEN 'robotics'
 WHEN normalized LIKE ANY(ARRAY[
  '%media production%','%news media%','%journalism%','%video streaming%','%film production%','%digital content%','%content creation%','%social media%','%журналистик%','%медиапроизводств%','%цифров%контент%','%производств%видео%','%социальн%сет%','%видеостриминг%'
 ]) THEN 'media'
 WHEN normalized LIKE ANY(ARRAY[
  '%telecom%','%wireless communication%','%mobile communication%','%cellular network%','% 5g %','% 6g %','%radio access network%','%optical communication%','%телеком%','%беспроводн%связ%','%мобильн%связ%','%сотов%сет%','%радиодоступ%','%оптическ%связ%'
 ]) THEN 'telecom'
 WHEN normalized LIKE ANY(ARRAY[
  '%semiconductor%','%transistor%','%integrated circuit%','%microchip%','%microelectronic%','%chip design%','%chiplet%','%photonic chip%','%полупроводник%','%транзистор%','%интегральн%схем%','%микроэлектрон%','%микросхем%','%проектирован%чип%'
 ]) THEN 'semiconductors'
 WHEN normalized LIKE ANY(ARRAY[
  '%consumer electronic%','%smartphone%','%smartwatch%','%wearable device%','%smart glasses%','%headphone%','%потребительск%электроник%','%смартфон%','%умн%час%','%носим%устройств%','%умн%очк%','%наушник%'
 ]) THEN 'consumer_electronics'
 WHEN normalized LIKE ANY(ARRAY[
  '%cybersecur%','%cyber secur%','%information security%','%data security%','%network security%','%computer security%','%intrusion detection%','%malware%','%ransomware%','%cryptograph%','%access control%','%physical security%','%threat detection%','%кибербезопас%','%информационн%безопас%','%защит%данных%','%сетев%безопас%','%обнаружени%вторжен%','%вредоносн%программ%','%криптограф%','%контрол%доступ%'
 ]) THEN 'security'
 WHEN normalized LIKE ANY(ARRAY[
  '%material%','%metallurg%','%catalys%','%polymer%','%composite%','%ceramic%','%alloy%','%chemical%','%chemistry%','%nanostructur%','%nanoparticl%','% cement%','%concrete%','%материал%','%металлург%','%катализ%','%полимер%','%композит%','%керамик%','%сплав%','%хими%','%наночастиц%','%цемент%','%бетон%','%извлечени%меди%','%обогащени%руд%','%copper extraction%','%mineral processing%'
 ]) THEN 'materials'
 WHEN normalized LIKE ANY(ARRAY[
  '%manufactur%','%industrial%','%machining%','%3d print%','%predictive maintenance%','%fabrication%','%строительств%','%промышлен%','%производств%','%станк%','%аддитив%'
 ]) THEN 'industry'
 WHEN normalized LIKE ANY(ARRAY[
  '%computer%','%computing%','%software%','%algorithm%','%neural network%','%machine learning%','%deep learning%','%language model%','%artificial intelligence%','%information retrieval%','%database%','%retrieval augmented%','%компьютер%','%вычислен%','%программ%','%алгоритм%','%нейросет%','%машинн%обучен%','%языков%модел%','%искусственн%интеллект%','%large language%','%generative%','%генератив%','%ранжирован%','%retrieval%','%reinforcement learning%','%обучение с подкреплением%'
 ]) THEN 'computing'
 WHEN normalized LIKE ANY(ARRAY[
  '%econom%','%financ%','%banking%','%monetary%','%inflation%','%social polic%','%public polic%','%demograph%','%sociolog%','%демограф%','%эконом%','%финанс%','%инфляц%','%социальн%политик%','%общественн%политик%','%социолог%'
 ]) THEN 'economics'
 WHEN normalized LIKE ANY(ARRAY[
  '%quantum%','%physics%','%mathematic%','%topolog%','%algebra%','%theorem%','%particle%','%fluid dynamic%','%radiocarbon%','%квант%','%физик%','%математ%','%тополог%','%алгебр%','%теорем%','%гидродинам%','%радиоуглерод%'
 ]) THEN 'science'
 ELSE '' END;
END;
$$;
-- +goose StatementEnd
