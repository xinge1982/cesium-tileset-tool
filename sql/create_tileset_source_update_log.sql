-- Audit log for updates made through TilesetSourceController.Update.
-- Run this statement in the same PostgreSQL database used by cesium-tileset-tool
-- before enabling the update endpoint.
CREATE TABLE IF NOT EXISTS public.tileset_source_update_log
(
    id           bigserial PRIMARY KEY,
    tileset_key  varchar(255)             NOT NULL,
    source_id    varchar(255)             NOT NULL,
    table_schema varchar(63)              NOT NULL,
    table_name   varchar(63)              NOT NULL,
    record_key   text                     NOT NULL,
    old_values   jsonb                    NOT NULL DEFAULT '{}'::jsonb,
    new_values   jsonb                    NOT NULL DEFAULT '{}'::jsonb,
    client_ip    inet,
    user_agent   text,
    created_at   timestamp with time zone NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE public.tileset_source_update_log IS
    'Audit log of source data updates made by cesium-tileset-tool';
COMMENT ON COLUMN public.tileset_source_update_log.record_key IS
    'Text representation of the source table primary key';
COMMENT ON COLUMN public.tileset_source_update_log.old_values IS
    'Configured submitted field values before the update';
COMMENT ON COLUMN public.tileset_source_update_log.new_values IS
    'Configured submitted field values after the update';

CREATE INDEX IF NOT EXISTS idx_tileset_source_update_log_record
    ON public.tileset_source_update_log
        (table_schema, table_name, record_key, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_tileset_source_update_log_created_at
    ON public.tileset_source_update_log (created_at DESC);
