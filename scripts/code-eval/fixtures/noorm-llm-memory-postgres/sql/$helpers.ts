/**
 * Quote a SQL identifier (table/column name) using double quotes,
 * escaping any embedded double quotes by doubling them.
 */
export function quoteIdent(name: string): string {

    return '"' + name.replace(/"/g, '""') + '"';
}

/**
 * Render a JS array as a PostgreSQL ARRAY literal of text values.
 * Empty arrays render as `ARRAY[]::text[]` so the type is unambiguous.
 */
export function pgArray(values: unknown[]): string {

    if (values.length === 0) {

        return 'ARRAY[]::text[]';
    }

    const literals = values.map((v) => {

        if (v === null || v === undefined) return 'NULL';
        if (typeof v === 'number' || typeof v === 'boolean') return String(v);

        return "'" + String(v).replace(/'/g, "''") + "'";
    });

    return 'ARRAY[' + literals.join(', ') + ']';
}

/**
 * Render a JS boolean as a PostgreSQL boolean literal (`true` / `false`).
 */
export function boolLit(value: boolean): string {

    return value ? 'true' : 'false';
}
