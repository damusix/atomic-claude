-- Triggers: StateTransition exclusive-subtype enforcement
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES [S] — StateTransition)
--
-- Each StateTransition belongs to exactly ONE *_StateTransition subtype, and
-- that subtype must be compatible with StateTransition.state_transition_type:
--
--   'milestone-tracking'  -> Milestone_StateTransition
--   'milestone-relevance' -> Milestone_StateTransition
--   'task-tracking'       -> Task_StateTransition
--   'memory-relevance'    -> Memory_StateTransition
--   'note-relevance'      -> Note_StateTransition
--   'artifact-relevance'  -> Artifact_StateTransition
--
-- Milestone_StateTransition is the only subtype that handles two discriminator
-- values (tracking + relevance). PostgreSQL has no cross-table CHECK, so we
-- enforce via AFTER INSERT/UPDATE triggers on each subtype table plus an
-- AFTER UPDATE trigger on StateTransition.state_transition_type.

-- -----------------------------------------------------------------------------
-- Trigger function: validate a *_StateTransition row against
-- StateTransition.state_transition_type and ensure transition_id is not
-- present in any of the OTHER *_StateTransition tables.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION "trg_check_state_transition_exclusivity"()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    declared_type   VARCHAR(32);
    expected_types  VARCHAR(32)[];
    found_in_other  INT;
BEGIN
    -- Determine the allowed state_transition_type values for this trigger's table.
    expected_types := CASE TG_TABLE_NAME
        WHEN 'Milestone_StateTransition' THEN ARRAY['milestone-tracking', 'milestone-relevance']
        WHEN 'Task_StateTransition'      THEN ARRAY['task-tracking']
        WHEN 'Memory_StateTransition'    THEN ARRAY['memory-relevance']
        WHEN 'Note_StateTransition'      THEN ARRAY['note-relevance']
        WHEN 'Artifact_StateTransition'  THEN ARRAY['artifact-relevance']
        ELSE NULL
    END;

    IF expected_types IS NULL THEN
        RAISE EXCEPTION 'trg_check_state_transition_exclusivity: unknown trigger table %', TG_TABLE_NAME;
    END IF;

    -- Pull the basetype's declared type.
    SELECT state_transition_type
    INTO declared_type
    FROM "StateTransition"
    WHERE transition_id = NEW.transition_id;

    IF declared_type IS NULL THEN
        RAISE EXCEPTION 'trg_check_state_transition_exclusivity: transition_id % does not exist in StateTransition',
            NEW.transition_id;
    END IF;

    IF NOT (declared_type = ANY(expected_types)) THEN
        RAISE EXCEPTION 'trg_check_state_transition_exclusivity: transition_id % has state_transition_type=% but is being written into % (allowed: %)',
            NEW.transition_id, declared_type, TG_TABLE_NAME, expected_types;
    END IF;

    -- Verify the row doesn't already exist in another *_StateTransition subtype table.
    SELECT
        (CASE WHEN TG_TABLE_NAME <> 'Milestone_StateTransition' AND EXISTS (SELECT 1 FROM "Milestone_StateTransition" WHERE transition_id = NEW.transition_id) THEN 1 ELSE 0 END) +
        (CASE WHEN TG_TABLE_NAME <> 'Task_StateTransition'      AND EXISTS (SELECT 1 FROM "Task_StateTransition"      WHERE transition_id = NEW.transition_id) THEN 1 ELSE 0 END) +
        (CASE WHEN TG_TABLE_NAME <> 'Memory_StateTransition'    AND EXISTS (SELECT 1 FROM "Memory_StateTransition"    WHERE transition_id = NEW.transition_id) THEN 1 ELSE 0 END) +
        (CASE WHEN TG_TABLE_NAME <> 'Note_StateTransition'      AND EXISTS (SELECT 1 FROM "Note_StateTransition"      WHERE transition_id = NEW.transition_id) THEN 1 ELSE 0 END) +
        (CASE WHEN TG_TABLE_NAME <> 'Artifact_StateTransition'  AND EXISTS (SELECT 1 FROM "Artifact_StateTransition"  WHERE transition_id = NEW.transition_id) THEN 1 ELSE 0 END)
    INTO found_in_other;

    IF found_in_other > 0 THEN
        RAISE EXCEPTION 'trg_check_state_transition_exclusivity: transition_id % already exists in another *_StateTransition subtype table (write attempted on %)',
            NEW.transition_id, TG_TABLE_NAME;
    END IF;

    RETURN NEW;
END;
$$;

-- -----------------------------------------------------------------------------
-- Trigger function: when StateTransition.state_transition_type is updated,
-- ensure the new type is still compatible with whichever *_StateTransition
-- subtype table currently holds the transition_id.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION "trg_check_state_transition_type_update"()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    holding_table   VARCHAR(32);
    allowed_types   VARCHAR(32)[];
BEGIN
    -- Skip when state_transition_type is unchanged.
    IF NEW.state_transition_type IS NOT DISTINCT FROM OLD.state_transition_type THEN
        RETURN NEW;
    END IF;

    -- Locate which (if any) subtype table currently holds this transition_id.
    IF EXISTS (SELECT 1 FROM "Milestone_StateTransition" WHERE transition_id = NEW.transition_id) THEN
        holding_table := 'Milestone_StateTransition';
        allowed_types := ARRAY['milestone-tracking', 'milestone-relevance'];
    ELSIF EXISTS (SELECT 1 FROM "Task_StateTransition" WHERE transition_id = NEW.transition_id) THEN
        holding_table := 'Task_StateTransition';
        allowed_types := ARRAY['task-tracking'];
    ELSIF EXISTS (SELECT 1 FROM "Memory_StateTransition" WHERE transition_id = NEW.transition_id) THEN
        holding_table := 'Memory_StateTransition';
        allowed_types := ARRAY['memory-relevance'];
    ELSIF EXISTS (SELECT 1 FROM "Note_StateTransition" WHERE transition_id = NEW.transition_id) THEN
        holding_table := 'Note_StateTransition';
        allowed_types := ARRAY['note-relevance'];
    ELSIF EXISTS (SELECT 1 FROM "Artifact_StateTransition" WHERE transition_id = NEW.transition_id) THEN
        holding_table := 'Artifact_StateTransition';
        allowed_types := ARRAY['artifact-relevance'];
    ELSE
        -- No subtype row yet — allow the type change. The subtype trigger will
        -- enforce consistency when the matching subtype row is inserted.
        RETURN NEW;
    END IF;

    IF NOT (NEW.state_transition_type = ANY(allowed_types)) THEN
        RAISE EXCEPTION 'trg_check_state_transition_type_update: transition_id % is held in % (allowed types: %) but state_transition_type was changed to %',
            NEW.transition_id, holding_table, allowed_types, NEW.state_transition_type;
    END IF;

    RETURN NEW;
END;
$$;

-- -----------------------------------------------------------------------------
-- Bind subtype-table triggers (idempotent).
-- -----------------------------------------------------------------------------
DROP TRIGGER IF EXISTS trg_milestone_state_transition_exclusivity ON "Milestone_StateTransition";
CREATE TRIGGER trg_milestone_state_transition_exclusivity
    AFTER INSERT OR UPDATE ON "Milestone_StateTransition"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_state_transition_exclusivity"();

DROP TRIGGER IF EXISTS trg_task_state_transition_exclusivity      ON "Task_StateTransition";
CREATE TRIGGER trg_task_state_transition_exclusivity
    AFTER INSERT OR UPDATE ON "Task_StateTransition"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_state_transition_exclusivity"();

DROP TRIGGER IF EXISTS trg_memory_state_transition_exclusivity    ON "Memory_StateTransition";
CREATE TRIGGER trg_memory_state_transition_exclusivity
    AFTER INSERT OR UPDATE ON "Memory_StateTransition"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_state_transition_exclusivity"();

DROP TRIGGER IF EXISTS trg_note_state_transition_exclusivity      ON "Note_StateTransition";
CREATE TRIGGER trg_note_state_transition_exclusivity
    AFTER INSERT OR UPDATE ON "Note_StateTransition"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_state_transition_exclusivity"();

DROP TRIGGER IF EXISTS trg_artifact_state_transition_exclusivity  ON "Artifact_StateTransition";
CREATE TRIGGER trg_artifact_state_transition_exclusivity
    AFTER INSERT OR UPDATE ON "Artifact_StateTransition"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_state_transition_exclusivity"();

-- -----------------------------------------------------------------------------
-- Bind basetype-table trigger (idempotent).
-- -----------------------------------------------------------------------------
DROP TRIGGER IF EXISTS trg_state_transition_type_update ON "StateTransition";
CREATE TRIGGER trg_state_transition_type_update
    AFTER UPDATE OF state_transition_type ON "StateTransition"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_state_transition_type_update"();
