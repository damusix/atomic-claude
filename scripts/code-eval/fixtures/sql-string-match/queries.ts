// sql-string-match fixture (C7): Kysely-style query builder usage against
// schema.sql's table/view/procedure, plus deliberate negative cases.

function anchoredQueries(db: any) {
  // High confidence: vocabulary callee ("selectFrom") + object-name match
  // against the view "active_orders".
  db.selectFrom("active_orders")
    // High confidence: vocabulary callee ("innerJoin") + object-name match
    // against the table "orders_tbl". Also anchors this owner scope for the
    // bare-column match below (views yield no column nodes, so the anchor
    // that actually resolves is the table, not the view).
    .innerJoin("orders_tbl", "orders_tbl.status", "active_orders.status")
    // Bare column string in anchored scope: "status" is a column of
    // orders_tbl, the table anchor gained above.
    .select("status")
    .execute();

  // C8 fragment tier — where-fragment: fails the C1 identifier shape (has a
  // "= ?" comparison + placeholder) but passes the fragment gate. Tokenizes
  // to the bare column "status"; anchored bare-column match computes low,
  // fragment demotion leaves it at the low floor.
  db.where("status = ?");

  // C8 fragment tier — order-DESC: qualified pair "orders_tbl.total" survives
  // tokenization ("DESC" is stoplisted). Qualified-column match computes
  // medium, fragment demotion drops it to low.
  db.orderBy("orders_tbl.total DESC");

  // C8 fragment tier — comma-separated pluck list: two bare columns of the
  // same anchored table, both low (fragment demotion of an already-low tier).
  db.select("order_id, status");

  // C8 fragment negative: passes the fragment gate (comparison operator) but
  // neither tokenized identifier ("error", "timeout") names any SQL object —
  // must produce zero edges.
  db.where("error = timeout");
}

function proseDecoyQueries(db: any) {
  // C8 prose-collision positive: "error = retries" passes the fragment gate
  // (comparison operator) and tokenizes to bare identifiers "error"/"retries".
  // "retries" happens to name a real decoy table (schema.sql) while "error"
  // names nothing — pass A object matching resolves "retries" to that table
  // via an empty/non-vocabulary CalleeExpr ("where"), computing medium, then
  // fragment demotion drops it to low. This is the documented, accepted
  // tradeoff of tokenizing fragments: a same-named object anywhere in the
  // index can produce a spurious low-confidence edge.
  db.where("error = retries");
}

function unanchoredQueries(db: any) {
  // Negative: same column name as above, but this owner scope never matched
  // a table/view via pass A — no anchor, so this must not produce an edge.
  db.select("status").execute();

  // Negative: prose string — fails the identifier-shape gate outright, never
  // even becomes a speculative sql_string ref.
  const note = "this is just a plain sentence, not an identifier";

  // Negative: identifier-shaped string that names no SQL object at all.
  const ghost = "totally_unknown_object";
}
