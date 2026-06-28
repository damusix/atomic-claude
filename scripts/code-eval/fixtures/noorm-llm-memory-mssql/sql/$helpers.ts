/**
 * Project-wide template helpers for the llm-memory-db-mssql schema build.
 *
 * These functions are auto-discovered by the noorm template engine. Any
 * `.sql.tmpl` file under `sql/` exposes them as `$.bracketed(...)`,
 * `$.bitLit(...)`, and `$.nstr(...)` alongside the built-in helpers
 * (`$.quote`, `$.escape`, etc.).
 *
 * The seed templates in `sql/05_seeds/` lean on `$.quote(...)` for string
 * literals because seeds work on `VARCHAR(32)` reference PKs where the
 * non-Unicode `'...'` form is correct. `$.nstr(...)` is the N-prefixed
 * counterpart for the rare case where a seed needs an `NVARCHAR` literal
 * (e.g. the sentinel rows that populate `NVARCHAR(255)` columns), and
 * `$.bracketed(...)` / `$.bitLit(...)` exist so future templates that
 * generate dynamic identifier or BIT literals stay consistent.
 */


/**
 * Wrap an identifier in MSSQL square brackets.
 *
 * @example
 *     $.bracketed('Memory_Tag') // -> '[Memory_Tag]'
 */
export function bracketed(name: string): string {

    return `[${name}]`;
}


/**
 * Render a JS boolean as a T-SQL BIT literal.
 *
 * @example
 *     $.bitLit(true)  // -> '1'
 *     $.bitLit(false) // -> '0'
 */
export function bitLit(value: boolean): string {

    return value ? '1' : '0';
}


/**
 * Wrap a string as an N-prefixed (Unicode) T-SQL literal, escaping any
 * embedded single quotes by doubling them.
 *
 * Use this when the target column is `NVARCHAR(...)` and you want the
 * literal flagged as Unicode at parse time. For plain `VARCHAR` reference
 * values prefer `$.quote(...)`.
 *
 * @example
 *     $.nstr("O'Reilly") // -> "N'O''Reilly'"
 */
export function nstr(value: string): string {

    return `N'${value.replace(/'/g, "''")}'`;
}
