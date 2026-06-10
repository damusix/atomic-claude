<?php

$ddl = <<<SQL
CREATE TABLE orders (id INT PRIMARY KEY, total DECIMAL(10,2) NOT NULL)
SQL;

function fetchOrder($id, $t) {
    $q1 = "SELECT id, total FROM orders WHERE id = ?";
    $q2 = "SELECT id, total FROM $t WHERE id = 1";
    return $q1;
}
