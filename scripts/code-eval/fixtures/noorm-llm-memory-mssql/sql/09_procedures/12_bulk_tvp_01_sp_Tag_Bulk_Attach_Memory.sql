-- =============================================================================
-- Bulk TVP procedures
-- -----------------------------------------------------------------------------
-- These procs accept Table-Valued Parameters declared in sql/00_types and
-- collapse what would otherwise be N round trips into a single set-based
-- statement. Each is idempotent on its target table's PK.
--
-- sp_Tag_Bulk_Attach_Memory  — set-based Memory_Tag attach from (tag_id, memory_id) pairs
-- sp_Memory_Bulk_Touch       — bump last_accessed_at and access_count for many memories
-- sp_Task_Bulk_Depend        — register many task dependencies, validating each
--                              row against the verb table and the cycle check
-- =============================================================================


CREATE OR ALTER PROCEDURE [dbo].[sp_Tag_Bulk_Attach_Memory]
    @Pairs [dbo].[TagAttachmentInput] READONLY
AS
BEGIN
    SET NOCOUNT ON;

    -- Idempotent: skip rows whose (tag_id, memory_id) combination already exists.
    INSERT INTO [dbo].[Memory_Tag] ([tag_id], [memory_id])
    SELECT p.[tag_id], p.[entity_id]
    FROM @Pairs p
    WHERE NOT EXISTS (
        SELECT 1 FROM [dbo].[Memory_Tag] mt
        WHERE mt.[tag_id]    = p.[tag_id]
          AND mt.[memory_id] = p.[entity_id]
    );
END
