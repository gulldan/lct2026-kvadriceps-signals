-- +goose Up
-- The observation unit becomes the technology, not the work. A revision is a
-- documents row; every mention, quote and event hangs off the revision that
-- carries it, so support can never be counted from a document it is not in.
CREATE TABLE technologies (
    id TEXT PRIMARY KEY,
    canonical_name TEXT NOT NULL CHECK (length(canonical_name) BETWEEN 1 AND 512),
    name_kind TEXT NOT NULL CHECK (name_kind IN ('authored', 'descriptive')),
    description TEXT NOT NULL DEFAULT '',
    validation_status TEXT NOT NULL CHECK (validation_status IN ('proposed', 'verified', 'needs_review')),
    method_version TEXT NOT NULL CHECK (length(method_version) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE technology_aliases (
    technology_id TEXT NOT NULL REFERENCES technologies(id) ON DELETE CASCADE,
    value TEXT NOT NULL CHECK (length(value) BETWEEN 1 AND 512),
    language TEXT NOT NULL CHECK (language IN ('ru', 'en')),
    basis TEXT NOT NULL CHECK (basis IN ('source_text', 'normalization', 'translation', 'expert_review')),
    PRIMARY KEY (technology_id, value, language)
);

-- A fully processed revision that yielded nothing is a record, not a missing
-- row: the denominator E counts it, and `no_technology` differs from `failed`.
CREATE TABLE technology_extractions (
    source_revision_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    extractor_version TEXT NOT NULL CHECK (length(extractor_version) BETWEEN 1 AND 128),
    schema_version TEXT NOT NULL CHECK (length(schema_version) BETWEEN 1 AND 128),
    status TEXT NOT NULL CHECK (status IN ('complete', 'partial', 'no_technology', 'needs_review', 'failed')),
    reason_code TEXT NOT NULL DEFAULT '',
    processed_sections INTEGER NOT NULL DEFAULT 0 CHECK (processed_sections >= 0),
    total_sections INTEGER NOT NULL DEFAULT 0 CHECK (total_sections >= 0),
    extracted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (source_revision_id, extractor_version, schema_version),
    CHECK (processed_sections <= total_sections),
    CHECK (status <> 'no_technology' OR processed_sections = total_sections)
);

CREATE TABLE technology_mentions (
    id TEXT PRIMARY KEY,
    technology_id TEXT NOT NULL REFERENCES technologies(id) ON DELETE CASCADE,
    source_revision_id TEXT NOT NULL,
    extractor_version TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 512),
    description TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('proposed_contribution', 'evaluated_application', 'claimed_invention',
        'product_implementation', 'deployment_report', 'background', 'prior_art', 'baseline',
        'citation_only', 'marketing_mention', 'uncertain')),
    problem TEXT NOT NULL DEFAULT '',
    mechanism TEXT NOT NULL DEFAULT '',
    inputs TEXT[] NOT NULL DEFAULT '{}',
    outputs TEXT[] NOT NULL DEFAULT '{}',
    extraction_status TEXT NOT NULL CHECK (extraction_status IN ('complete', 'partial', 'needs_review', 'unresolved_mention')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, source_revision_id),
    FOREIGN KEY (source_revision_id, extractor_version, schema_version)
        REFERENCES technology_extractions(source_revision_id, extractor_version, schema_version) ON DELETE CASCADE
);
CREATE INDEX technology_mentions_technology ON technology_mentions(technology_id);
CREATE INDEX technology_mentions_revision ON technology_mentions(source_revision_id);

-- The composite key carries the revision, so a quote can only be attached to a
-- mention of the same revision it was read from.
CREATE TABLE technology_evidence (
    id TEXT PRIMARY KEY,
    mention_id TEXT NOT NULL,
    source_revision_id TEXT NOT NULL,
    section TEXT NOT NULL DEFAULT '',
    page INTEGER CHECK (page IS NULL OR page > 0),
    start_char INTEGER NOT NULL CHECK (start_char >= 0),
    end_char INTEGER NOT NULL,
    quote TEXT NOT NULL CHECK (length(quote) BETWEEN 1 AND 8192),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_char > start_char),
    UNIQUE (id, mention_id),
    FOREIGN KEY (mention_id, source_revision_id)
        REFERENCES technology_mentions(id, source_revision_id) ON DELETE CASCADE
);
CREATE INDEX technology_evidence_mention ON technology_evidence(mention_id);

CREATE TABLE technology_field_evidence (
    mention_id TEXT NOT NULL,
    field TEXT NOT NULL CHECK (field IN ('name', 'description', 'role', 'problem', 'mechanism', 'inputs', 'outputs')),
    evidence_id TEXT NOT NULL,
    PRIMARY KEY (mention_id, field, evidence_id),
    FOREIGN KEY (evidence_id, mention_id) REFERENCES technology_evidence(id, mention_id) ON DELETE CASCADE
);

-- Symmetric relations are one fact: the ordered pair is the storage form, so
-- (B,A) is rejected rather than silently stored beside (A,B).
CREATE TABLE technology_relations (
    technology_id TEXT NOT NULL REFERENCES technologies(id) ON DELETE CASCADE,
    related_technology_id TEXT NOT NULL REFERENCES technologies(id) ON DELETE CASCADE,
    relation TEXT NOT NULL CHECK (relation IN ('same_as', 'variant_of', 'part_of', 'uses', 'competes_with', 'applies_to')),
    basis TEXT NOT NULL CHECK (length(basis) BETWEEN 1 AND 2048),
    decision_version TEXT NOT NULL CHECK (length(decision_version) BETWEEN 1 AND 128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (technology_id, related_technology_id, relation),
    CHECK (technology_id <> related_technology_id),
    CHECK (relation NOT IN ('same_as', 'competes_with') OR technology_id < related_technology_id)
);
CREATE INDEX technology_relations_related ON technology_relations(related_technology_id);

CREATE TABLE evidence_events (
    id TEXT PRIMARY KEY,
    technology_id TEXT NOT NULL REFERENCES technologies(id) ON DELETE CASCADE,
    source_revision_id TEXT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL CHECK (event_type IN ('research', 'claimed_invention', 'implementation',
        'release', 'pilot', 'deployment')),
    event_date DATE,
    date_precision TEXT NOT NULL CHECK (date_precision IN ('day', 'month', 'year', 'unknown')),
    organization TEXT NOT NULL DEFAULT '',
    evidence_group TEXT NOT NULL CHECK (length(evidence_group) BETWEEN 1 AND 512),
    modality TEXT NOT NULL CHECK (modality IN ('reported_measurement', 'author_claim', 'planned')),
    depends_on_event_id TEXT REFERENCES evidence_events(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((event_date IS NULL) = (date_precision = 'unknown')),
    CHECK (depends_on_event_id IS NULL OR depends_on_event_id <> id)
);
CREATE INDEX evidence_events_technology ON evidence_events(technology_id, event_date);
CREATE INDEX evidence_events_group ON evidence_events(evidence_group);

CREATE TABLE technology_reviews (
    id TEXT PRIMARY KEY,
    technology_id TEXT NOT NULL REFERENCES technologies(id) ON DELETE CASCADE,
    author_id TEXT NOT NULL CHECK (length(author_id) BETWEEN 1 AND 128),
    operation TEXT NOT NULL CHECK (operation IN ('rename', 'describe', 'set_role', 'set_dependency',
        'confirm', 'merge', 'split', 'revert')),
    reason TEXT NOT NULL CHECK (length(reason) BETWEEN 1 AND 2048),
    previous_value JSONB NOT NULL,
    new_value JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX technology_reviews_technology ON technology_reviews(technology_id, created_at DESC);

-- A technology exists because a source says so. Deferred: the mention is
-- inserted after its technology inside the same transaction.
-- +goose StatementBegin
CREATE FUNCTION technology_requires_mention() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM technology_mentions WHERE technology_id = NEW.id) THEN
        RAISE EXCEPTION 'technology % has no mention', NEW.id USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER technologies_require_mention AFTER INSERT ON technologies
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION technology_requires_mention();

-- +goose Down
-- Expert reviews and merge history are not re-derivable by rerunning extraction.
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Preserve technology reviews and evidence; roll back the compatible reader instead';
END $$;
-- +goose StatementEnd
