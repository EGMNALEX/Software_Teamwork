-- Local integration contract: optional cleanup companion for local demo seed
-- data; keep it in sync with deploy/seeds/001-local-demo-seed.sql.
\connect qa_system

DELETE FROM mcp_servers
WHERE id = '33333333-3333-4333-8333-333333333601'::uuid
  AND alias = 'document'
  AND created_by_user_id = 'local-seed';

DELETE FROM qa_config_knowledge_bases
WHERE external_kb_id = 'kb_local_demo';

DELETE FROM citations
WHERE message_id IN (
      '33333333-3333-4333-8333-333333333401'::uuid,
      '33333333-3333-4333-8333-333333333402'::uuid
  )
   OR response_run_id IN (
      SELECT id
      FROM response_runs
      WHERE conversation_id = '33333333-3333-4333-8333-333333333301'::uuid
  );

DELETE FROM response_stream_events
WHERE response_run_id IN (
    SELECT id
    FROM response_runs
    WHERE conversation_id = '33333333-3333-4333-8333-333333333301'::uuid
);

DELETE FROM response_process_steps
WHERE response_run_id IN (
    SELECT id
    FROM response_runs
    WHERE conversation_id = '33333333-3333-4333-8333-333333333301'::uuid
);

DELETE FROM agent_tool_calls
WHERE response_run_id IN (
    SELECT id
    FROM response_runs
    WHERE conversation_id = '33333333-3333-4333-8333-333333333301'::uuid
);

DELETE FROM agent_model_invocations
WHERE response_run_id IN (
    SELECT id
    FROM response_runs
    WHERE conversation_id = '33333333-3333-4333-8333-333333333301'::uuid
);

DELETE FROM response_runs
WHERE conversation_id = '33333333-3333-4333-8333-333333333301'::uuid;

DELETE FROM message_content_blocks
WHERE id IN (
    '33333333-3333-4333-8333-333333333501'::uuid,
    '33333333-3333-4333-8333-333333333502'::uuid
);

DELETE FROM messages
WHERE id IN (
    '33333333-3333-4333-8333-333333333401'::uuid,
    '33333333-3333-4333-8333-333333333402'::uuid
);

DELETE FROM conversations
WHERE id = '33333333-3333-4333-8333-333333333301'::uuid
  AND external_user_id = 'usr_local_admin';

\connect document_system

UPDATE report_types
SET default_template_id = null,
    updated_at = now()
WHERE (code = 'summer_peak_inspection' AND default_template_id = '11111111-1111-4111-8111-111111111101'::uuid)
   OR (code = 'coal_inventory_audit' AND default_template_id = '11111111-1111-4111-8111-111111111102'::uuid);

DELETE FROM report_operation_logs
WHERE id = '22222222-2222-4222-8222-222222222801'::uuid
   OR (target_type = 'report' AND target_id = '22222222-2222-4222-8222-222222222301');

DELETE FROM report_events
WHERE id = '22222222-2222-4222-8222-222222222701'::uuid
   OR report_id = '22222222-2222-4222-8222-222222222301'::uuid;

DELETE FROM report_files
WHERE report_id = '22222222-2222-4222-8222-222222222301'::uuid;

DELETE FROM report_job_attempts
WHERE job_id IN (
    SELECT id
    FROM report_jobs
    WHERE report_id = '22222222-2222-4222-8222-222222222301'::uuid
);

DELETE FROM report_jobs
WHERE report_id = '22222222-2222-4222-8222-222222222301'::uuid;

DELETE FROM report_section_versions
WHERE id IN (
      '22222222-2222-4222-8222-222222222601'::uuid,
      '22222222-2222-4222-8222-222222222602'::uuid
  )
   OR report_id = '22222222-2222-4222-8222-222222222301'::uuid;

DELETE FROM report_sections
WHERE id IN (
      '22222222-2222-4222-8222-222222222501'::uuid,
      '22222222-2222-4222-8222-222222222502'::uuid
  )
   OR report_id = '22222222-2222-4222-8222-222222222301'::uuid;

DELETE FROM report_outlines
WHERE id = '22222222-2222-4222-8222-222222222401'::uuid
   OR report_id = '22222222-2222-4222-8222-222222222301'::uuid;

DELETE FROM report_template_materials
WHERE id = '22222222-2222-4222-8222-222222222202'::uuid
   OR material_id = '22222222-2222-4222-8222-222222222201'::uuid;

DELETE FROM reports
WHERE id = '22222222-2222-4222-8222-222222222301'::uuid
  AND source = 'local_seed';

DELETE FROM report_materials
WHERE id = '22222222-2222-4222-8222-222222222201'::uuid
  AND created_by = 'usr_local_admin';

\connect knowledge_system

DELETE FROM document_chunks
WHERE id = 'chunk_local_demo_seed_001'
   OR document_id = 'doc_local_demo_seed';

DELETE FROM processing_jobs
WHERE document_id = 'doc_local_demo_seed'
   OR knowledge_base_id = 'kb_local_demo';

DELETE FROM knowledge_documents
WHERE id = 'doc_local_demo_seed'
  AND created_by = 'usr_local_admin';

DELETE FROM knowledge_bases
WHERE id = 'kb_local_demo'
  AND created_by = 'usr_local_admin';

\connect auth_system

DELETE FROM session_revocations
WHERE user_id IN ('usr_local_admin', 'usr_local_super_admin');

DELETE FROM auth_security_events
WHERE user_id IN ('usr_local_admin', 'usr_local_super_admin')
   OR username_snapshot IN ('admin', 'superadmin');

DELETE FROM auth_sessions
WHERE user_id IN ('usr_local_admin', 'usr_local_super_admin');

DELETE FROM user_roles
WHERE user_id IN ('usr_local_admin', 'usr_local_super_admin');

DELETE FROM auth_credentials
WHERE id IN ('cred_local_admin_password', 'cred_local_super_admin_password')
   OR user_id IN ('usr_local_admin', 'usr_local_super_admin');

DELETE FROM auth_users
WHERE id = 'usr_local_admin'
  AND username = 'admin';

DELETE FROM auth_users
WHERE id = 'usr_local_super_admin'
  AND username = 'superadmin';
