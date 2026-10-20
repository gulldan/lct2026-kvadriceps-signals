-- +goose Up
-- Прикладная область предшествует методу; пустая строка означает, что тема не определена.
-- Одна эвристика нужна обзору корпуса и чередованию прогнозов перед пагинацией.
-- +goose StatementBegin
CREATE FUNCTION research_topic(input text) RETURNS text
LANGUAGE plpgsql IMMUTABLE PARALLEL SAFE AS $$
DECLARE normalized text := ' ' || lower(coalesce(input,''));
BEGIN
 RETURN CASE
 WHEN normalized LIKE ANY(ARRAY[
  '%clinical%','%patient%','%cancer%','%oncolog%','%therap%','%diagnos%','%vaccine%','%surgery%','%surgical%',
  '%disease%','%pharmac%','%drug discovery%','%health care%','%healthcare%','%medical%','%medicine%','%медицин%',
  '%клинич%','%пациент%','%онкол%','%диагност%','%терап%','%вакцин%','%заболев%','%лечени%'
]) THEN 'medicine'
 WHEN normalized LIKE ANY(ARRAY[
  '%agricultur%','%agronom%','%crop%','%soil%','%irrigat%','%fertiliz%','%livestock%','%aquacultur%','%food%',
  '%nutrition%','%cultivated meat%','%plant breeding%','%сельскохозяй%','%агро%','%урожай%','%почв%','%орошени%',
  '%удобрени%','%пищев%','%продовольств%'
]) THEN 'agriculture'
 WHEN normalized LIKE ANY(ARRAY[
  '%energy%','%battery%','%batteries%','%solar cell%','%photovoltaic%','%hydrogen%','%fuel cell%','%electrolys%',
  '%electrolyz%','%power grid%','%wind turbine%','%nuclear reactor%','%энерг%','%аккумулятор%','%батаре%',
  '%водород%','%топлив%','%электролиз%','%солнечн%','%электросет%'
]) THEN 'energy'
 WHEN normalized LIKE ANY(ARRAY[
  '%climate%','%environment%','%ecolog%','%biodiversity%','%carbon capture%','%carbon sequestration%',
  '%carbon emission%','%wastewater%','%pollution%','%recycl%','%desalinat%','%water treatment%','%ecosystem%',
  '%климат%','%эколог%','%биоразнообраз%','%декарбониз%','%очистк%вод%','%загрязнен%','%переработк%отход%'
]) THEN 'environment'
 WHEN normalized LIKE ANY(ARRAY[
  '%biotech%','%synthetic biology%','%genom%','%gene edit%','%crispr%','%proteom%','%microbiom%','%ferment%',
  '%enzyme%','%biomolecul%','%cellular%','%биотех%','%синтетическ%биолог%','%геном%','%редактирован%ген%',
  '%фермент%','%микробиом%','%белк%'
]) THEN 'biotechnology'
 WHEN normalized LIKE ANY(ARRAY[
  '%material%','%metallurg%','%catalys%','%polymer%','%composite%','%ceramic%','%alloy%','%chemical%',
  '%chemistry%','%nanostructur%','%nanoparticl%','% cement%','%concrete%','%материал%','%металлург%','%катализ%',
  '%полимер%','%композит%','%керамик%','%сплав%','%хими%','%наночастиц%','%цемент%','%бетон%'
]) THEN 'materials'
 WHEN normalized LIKE ANY(ARRAY[
  '%transport%','%vehicle%','%traffic%','%logistics%','%supply chain%','%autonomous driv%','%railway%','%aviation%',
  '%транспорт%','%автомобил%','%логистик%','%цепочк%постав%','%автономн%вожден%','%авиаци%','%железнодорож%'
]) THEN 'transport'
 WHEN normalized LIKE ANY(ARRAY[
  '%spacecraft%','%satellite%','%astronom%','%astrophys%','%cosmolog%','%exoplanet%','%galaxi%','%galaxy%',
  '%telescope%','%космич%','%спутник%','%астроном%','%астрофиз%','%космолог%','%галактик%','%телескоп%'
]) THEN 'space'
 WHEN normalized LIKE ANY(ARRAY[
  '%manufactur%','%industrial%','%robot%','%machining%','%3d print%','%predictive maintenance%',
  '%semiconductor%','%fabrication%','%строительств%','%промышлен%','%производств%','%робот%','%станк%',
  '%полупроводник%','%аддитив%'
]) THEN 'industry'
 WHEN normalized LIKE ANY(ARRAY[
  '%econom%','%financ%','%banking%','%monetary%','%inflation%','%education%','%social polic%','%public polic%',
  '%демограф%','%эконом%','%финанс%','%образован%','%инфляц%','%социальн%политик%'
]) THEN 'economics'
 WHEN normalized LIKE ANY(ARRAY[
  '%computer%','%computing%','%software%','%algorithm%','%neural network%','%machine learning%','%deep learning%',
  '%language model%','%artificial intelligence%','%cybersecur%','%information retrieval%','%database%',
  '%retrieval augmented%','%компьютер%','%вычислен%','%программ%','%алгоритм%','%нейросет%','%машинн%обучен%',
  '%языков%модел%','%искусственн%интеллект%','%кибербезопас%','%large language%','%generative%','%генератив%',
  '%ранжирован%','%retrieval%','%reinforcement learning%','%обучение с подкреплением%'
]) THEN 'computing'
 WHEN normalized LIKE ANY(ARRAY[
  '%quantum%','%physics%','%mathematic%','%topolog%','%algebra%','%theorem%','%particle%','%fluid dynamic%',
  '%квант%','%физик%','%математ%','%тополог%','%алгебр%','%теорем%','%гидродинам%'
]) THEN 'science'
 ELSE '' END;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION research_topic(text);
