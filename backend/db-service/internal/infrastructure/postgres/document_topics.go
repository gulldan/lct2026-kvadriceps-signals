package postgres

const documentTopicCTE = `, topic_documents AS (SELECT research_topic(topic_title) AS topic FROM filtered),
 topic_counts AS (SELECT topic,count(*) AS n FROM topic_documents GROUP BY topic) `

const documentTopicAggregate = `
 'topic_distribution',jsonb_build_object(
 'method','` + documentTopicMethod + `',
 'classified_documents',(SELECT count(*) FROM topic_documents WHERE topic<>''),
 'unclassified_documents',(SELECT count(*) FROM topic_documents WHERE topic=''),
 'topics',coalesce((SELECT jsonb_agg(jsonb_build_object('id',topic,'count',n) ORDER BY n DESC,topic)
 FROM topic_counts WHERE topic<>''),'[]')),
`
