class ProductRepo {
    val ddl = """CREATE TABLE reviews (id INT PRIMARY KEY, product_id INT, rating INT)"""

    fun fetch(id: Int, t: String) {
        val q1 = "SELECT id, rating FROM reviews WHERE id = $id"
        val q2 = "SELECT id, rating FROM $t WHERE id = 1"
    }
}
