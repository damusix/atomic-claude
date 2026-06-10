DDL = <<~SQL
  CREATE TABLE accounts (id INTEGER PRIMARY KEY, email TEXT NOT NULL, created_at TIMESTAMP)
SQL

def find_account(id, t)
  q1 = "SELECT id, email FROM accounts WHERE id = #{id}"
  q2 = "SELECT id, email FROM #{t} WHERE id = 1"
  q1
end
