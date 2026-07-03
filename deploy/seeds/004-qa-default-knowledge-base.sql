-- Local integration contract: link the seeded demo knowledge base to the
-- active QA configuration so local knowledge_qa questions can retrieve data.
\connect qa_system

INSERT INTO qa_config_knowledge_bases (
    config_id,
    external_kb_id,
    kb_type,
    display_name_snapshot,
    sort_order
)
SELECT
    q.id,
    'kb_local_demo',
    'local_demo',
    'Local Demo Knowledge Base',
    0
FROM qa_config_versions q
WHERE q.is_active = true
ORDER BY q.version_no DESC
LIMIT 1
ON CONFLICT (config_id, external_kb_id) DO UPDATE
SET kb_type = EXCLUDED.kb_type,
    display_name_snapshot = EXCLUDED.display_name_snapshot,
    sort_order = EXCLUDED.sort_order;
