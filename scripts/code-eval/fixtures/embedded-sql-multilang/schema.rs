static DDL: &str = r#"CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL, price REAL)"#;

fn get_product(id: i64) {
    let q = "SELECT id, name, price FROM products WHERE id = ?";
    let _ = q;
}
