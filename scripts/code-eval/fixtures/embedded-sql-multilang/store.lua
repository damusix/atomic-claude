local DDL = "CREATE TABLE items (id INTEGER PRIMARY KEY, label TEXT NOT NULL)"

local function getItem(db, id)
    local q = "SELECT id, label FROM items WHERE id = ?"
    return db:query(q)
end
