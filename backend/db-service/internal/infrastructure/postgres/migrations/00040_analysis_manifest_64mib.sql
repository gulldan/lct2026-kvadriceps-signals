-- +goose Up
-- The frozen analysis input carries every readable record with its provenance
-- and every collection page it may count against. With the harvester covering
-- all arXiv archives the manifest of a full corpus passed 16 MiB and Freeze
-- refused the analysis; the ceiling follows analysisdoc.MaxBytes (64 MiB).
ALTER TABLE signal_analysis_inputs DROP CONSTRAINT IF EXISTS signal_analysis_inputs_manifest_check;
ALTER TABLE signal_analysis_inputs ADD CONSTRAINT signal_analysis_inputs_manifest_check
    CHECK (octet_length(manifest) <= 67108864);

-- +goose Down
ALTER TABLE signal_analysis_inputs DROP CONSTRAINT IF EXISTS signal_analysis_inputs_manifest_check;
ALTER TABLE signal_analysis_inputs ADD CONSTRAINT signal_analysis_inputs_manifest_check
    CHECK (octet_length(manifest) <= 16777216);
