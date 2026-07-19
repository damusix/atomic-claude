-- sql-string-match fixture (C7): a table (with columns), a view, and a
-- procedure. The TypeScript fixture references these by string literal
-- rather than through host-language symbol calls, exercising the
-- string-match resolution pipeline (C2/C3).

CREATE TABLE orders_tbl (
    order_id SERIAL PRIMARY KEY,
    status   VARCHAR(20),
    total    NUMERIC(10, 2)
);

CREATE OR REPLACE VIEW active_orders AS
SELECT * FROM orders_tbl WHERE status = 'open';

CREATE OR REPLACE PROCEDURE archive_orders(p_id INT)
LANGUAGE plpgsql AS $$
BEGIN
  UPDATE orders_tbl SET status = 'archived' WHERE order_id = p_id;
END;
$$;

-- Decoy table for the C8 prose-collision case: exists only so a fragment
-- token that happens to share its name resolves via pass A object matching,
-- proving the tokenizer's collision risk is real rather than a name that
-- coincidentally matches nothing.
CREATE TABLE retries (id INT);
