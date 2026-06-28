-- Triggers: Note exclusive-subtype enforcement
-- Source: tmp/llm-memory-db.pseudo  (EXCLUSIVE SUBTYPES [S] — Note)
--
-- Each Note belongs to exactly ONE *_Note subtype table, and that table must
-- match Note.note_type. PostgreSQL has no cross-table CHECK constraints, so we
-- enforce this with AFTER INSERT/UPDATE triggers on each subtype table plus an
-- AFTER UPDATE trigger on Note.note_type. The procs already validate at insert
-- time; these triggers are defense-in-depth for direct DML.

-- -----------------------------------------------------------------------------
-- Trigger function: validate a *_Note row against Note.note_type and
-- ensure note_id is not present in any of the OTHER *_Note tables.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION "trg_check_note_exclusivity"()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    declared_type   VARCHAR(32);
    expected_type   VARCHAR(32);
    found_in_other  INT;
BEGIN
    -- Determine the expected note_type for the table this trigger fires on.
    expected_type := CASE TG_TABLE_NAME
        WHEN 'Project_Note'   THEN 'project'
        WHEN 'Milestone_Note' THEN 'milestone'
        WHEN 'Task_Note'      THEN 'task'
        ELSE NULL
    END;

    IF expected_type IS NULL THEN
        RAISE EXCEPTION 'trg_check_note_exclusivity: unknown trigger table %', TG_TABLE_NAME;
    END IF;

    -- Pull the basetype's declared type.
    SELECT note_type
    INTO declared_type
    FROM "Note"
    WHERE note_id = NEW.note_id;

    IF declared_type IS NULL THEN
        RAISE EXCEPTION 'trg_check_note_exclusivity: note_id % does not exist in Note', NEW.note_id;
    END IF;

    IF declared_type <> expected_type THEN
        RAISE EXCEPTION 'trg_check_note_exclusivity: note_id % has note_type=% but is being written into % (expected note_type=%)',
            NEW.note_id, declared_type, TG_TABLE_NAME, expected_type;
    END IF;

    -- Verify the row doesn't already exist in another *_Note subtype table.
    SELECT
        (CASE WHEN TG_TABLE_NAME <> 'Project_Note'   AND EXISTS (SELECT 1 FROM "Project_Note"   WHERE note_id = NEW.note_id) THEN 1 ELSE 0 END) +
        (CASE WHEN TG_TABLE_NAME <> 'Milestone_Note' AND EXISTS (SELECT 1 FROM "Milestone_Note" WHERE note_id = NEW.note_id) THEN 1 ELSE 0 END) +
        (CASE WHEN TG_TABLE_NAME <> 'Task_Note'      AND EXISTS (SELECT 1 FROM "Task_Note"      WHERE note_id = NEW.note_id) THEN 1 ELSE 0 END)
    INTO found_in_other;

    IF found_in_other > 0 THEN
        RAISE EXCEPTION 'trg_check_note_exclusivity: note_id % already exists in another *_Note subtype table (write attempted on %)',
            NEW.note_id, TG_TABLE_NAME;
    END IF;

    RETURN NEW;
END;
$$;

-- -----------------------------------------------------------------------------
-- Trigger function: when Note.note_type is updated, ensure the new type still
-- matches whichever *_Note subtype table currently holds the note_id.
-- -----------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION "trg_check_note_type_update"()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    holding_table   VARCHAR(32);
    expected_type   VARCHAR(32);
BEGIN
    -- Skip when note_type is unchanged.
    IF NEW.note_type IS NOT DISTINCT FROM OLD.note_type THEN
        RETURN NEW;
    END IF;

    -- Locate which (if any) subtype table currently holds this note_id.
    IF EXISTS (SELECT 1 FROM "Project_Note" WHERE note_id = NEW.note_id) THEN
        holding_table := 'Project_Note';
        expected_type := 'project';
    ELSIF EXISTS (SELECT 1 FROM "Milestone_Note" WHERE note_id = NEW.note_id) THEN
        holding_table := 'Milestone_Note';
        expected_type := 'milestone';
    ELSIF EXISTS (SELECT 1 FROM "Task_Note" WHERE note_id = NEW.note_id) THEN
        holding_table := 'Task_Note';
        expected_type := 'task';
    ELSE
        -- No subtype row yet — allow the type change. The subtype trigger will
        -- enforce consistency when the matching subtype row is inserted.
        RETURN NEW;
    END IF;

    IF NEW.note_type <> expected_type THEN
        RAISE EXCEPTION 'trg_check_note_type_update: note_id % is held in % (requires note_type=%) but note_type was changed to %',
            NEW.note_id, holding_table, expected_type, NEW.note_type;
    END IF;

    RETURN NEW;
END;
$$;

-- -----------------------------------------------------------------------------
-- Bind subtype-table triggers (idempotent).
-- -----------------------------------------------------------------------------
DROP TRIGGER IF EXISTS trg_project_note_exclusivity   ON "Project_Note";
CREATE TRIGGER trg_project_note_exclusivity
    AFTER INSERT OR UPDATE ON "Project_Note"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_note_exclusivity"();

DROP TRIGGER IF EXISTS trg_milestone_note_exclusivity ON "Milestone_Note";
CREATE TRIGGER trg_milestone_note_exclusivity
    AFTER INSERT OR UPDATE ON "Milestone_Note"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_note_exclusivity"();

DROP TRIGGER IF EXISTS trg_task_note_exclusivity      ON "Task_Note";
CREATE TRIGGER trg_task_note_exclusivity
    AFTER INSERT OR UPDATE ON "Task_Note"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_note_exclusivity"();

-- -----------------------------------------------------------------------------
-- Bind basetype-table trigger (idempotent).
-- -----------------------------------------------------------------------------
DROP TRIGGER IF EXISTS trg_note_type_update ON "Note";
CREATE TRIGGER trg_note_type_update
    AFTER UPDATE OF note_type ON "Note"
    FOR EACH ROW EXECUTE FUNCTION "trg_check_note_type_update"();
