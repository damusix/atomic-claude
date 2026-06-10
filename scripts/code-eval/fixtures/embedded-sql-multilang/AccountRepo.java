public class AccountRepo {
    static final String DDL = "CREATE TABLE sessions (id INT PRIMARY KEY, token VARCHAR(255) NOT NULL)";

    public void loadSession(int id) {
        String q = "SELECT id, token FROM sessions WHERE id = ?";
    }
}
