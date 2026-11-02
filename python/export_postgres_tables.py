#!/usr/bin/env python3
"""Export PostgreSQL/PostGIS tables to separate UTF-8 CSV files.

The geometry column is replaced by a leading ``WKT`` column. Connection
settings are read by libpq/psycopg from the standard PG* environment variables.
"""

from __future__ import annotations

import argparse
import sys
from dataclasses import dataclass
from pathlib import Path

try:
    import psycopg
    from psycopg import sql
except ImportError as exc:
    raise SystemExit(
        "Missing dependency: install it with "
        "`python -m pip install 'psycopg[binary]'`."
    ) from exc


@dataclass(frozen=True)
class TableName:
    schema: str
    table: str

    @property
    def display_name(self) -> str:
        return f"{self.schema}.{self.table}"


def parse_table_name(value: str, default_schema: str) -> TableName:
    """Parse an unquoted ``table`` or ``schema.table`` command-line value."""
    parts = value.split(".")
    if len(parts) == 1:
        schema, table = default_schema, parts[0]
    elif len(parts) == 2:
        schema, table = parts
    else:
        raise argparse.ArgumentTypeError(
            f"Invalid table name {value!r}; use table or schema.table"
        )

    if not schema or not table:
        raise argparse.ArgumentTypeError(f"Invalid table name {value!r}")
    return TableName(schema=schema, table=table)


def get_columns(conn: psycopg.Connection, table: TableName) -> list[tuple[str, str]]:
    """Return non-dropped user columns as (name, PostgreSQL type)."""
    query = """
        SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod)
        FROM pg_catalog.pg_attribute AS a
        JOIN pg_catalog.pg_class AS c ON c.oid = a.attrelid
        JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace
        WHERE n.nspname = %s
          AND c.relname = %s
          AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
          AND a.attnum > 0
          AND NOT a.attisdropped
        ORDER BY a.attnum
    """
    rows = conn.execute(query, (table.schema, table.table)).fetchall()
    if not rows:
        raise ValueError(f"Table or view not found, or it has no columns: {table.display_name}")
    return [(str(name), str(type_name)) for name, type_name in rows]


def build_copy_statement(
    table: TableName,
    columns: list[tuple[str, str]],
    geom_column: str,
) -> sql.Composed:
    column_types = dict(columns)
    if geom_column not in column_types:
        raise ValueError(
            f"Geometry column {geom_column!r} not found in {table.display_name}"
        )

    geom_type = column_types[geom_column].lower()
    if not (geom_type.startswith("geometry") or geom_type.startswith("geography")):
        raise ValueError(
            f"Column {table.display_name}.{geom_column} has type "
            f"{column_types[geom_column]!r}, not geometry/geography"
        )

    selected_columns: list[sql.Composable] = [
        sql.SQL("ST_AsText({}) AS {}").format(
            sql.Identifier(geom_column), sql.Identifier("WKT")
        )
    ]
    selected_columns.extend(
        sql.Identifier(name) for name, _ in columns if name != geom_column
    )

    select_query = sql.SQL("SELECT {} FROM {}.{}").format(
        sql.SQL(", ").join(selected_columns),
        sql.Identifier(table.schema),
        sql.Identifier(table.table),
    )
    return sql.SQL(
        "COPY ({}) TO STDOUT WITH "
        "(FORMAT CSV, HEADER TRUE, ENCODING 'UTF8', FORCE_QUOTE *)"
    ).format(select_query)


def export_table(
    conn: psycopg.Connection,
    table: TableName,
    output_path: Path,
    geom_column: str,
) -> None:
    columns = get_columns(conn, table)
    copy_statement = build_copy_statement(table, columns, geom_column)
    temporary_path = output_path.with_suffix(output_path.suffix + ".part")

    try:
        with conn.cursor() as cursor, temporary_path.open("wb") as output:
            with cursor.copy(copy_statement) as copy:
                while data := copy.read():
                    output.write(data)
        temporary_path.replace(output_path)
    except Exception:
        temporary_path.unlink(missing_ok=True)
        raise


def build_output_paths(tables: list[TableName], output_dir: Path) -> list[Path]:
    counts: dict[str, int] = {}
    for table in tables:
        counts[table.table] = counts.get(table.table, 0) + 1

    return [
        output_dir
        / (
            f"{table.schema}__{table.table}.csv"
            if counts[table.table] > 1
            else f"{table.table}.csv"
        )
        for table in tables
    ]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Export multiple PostgreSQL/PostGIS tables to separate CSV files; "
            "the geometry column is emitted as WKT."
        )
    )
    parser.add_argument(
        "tables",
        nargs="+",
        help="table names, each in table or schema.table form",
    )
    parser.add_argument(
        "-o",
        "--output-dir",
        required=True,
        type=Path,
        help="directory for generated CSV files",
    )
    parser.add_argument(
        "--schema",
        default="public",
        help="default schema for unqualified table names (default: public)",
    )
    parser.add_argument(
        "--geom-column",
        default="geom",
        help="geometry/geography column name (default: geom)",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        tables = [parse_table_name(value, args.schema) for value in args.tables]
    except argparse.ArgumentTypeError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2

    duplicates = [name for name in set(tables) if tables.count(name) > 1]
    if duplicates:
        names = ", ".join(sorted(table.display_name for table in duplicates))
        print(f"error: duplicate table arguments: {names}", file=sys.stderr)
        return 2

    output_dir: Path = args.output_dir.expanduser().resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    output_paths = build_output_paths(tables, output_dir)

    # An empty conninfo makes psycopg/libpq read PGHOST, PGPORT, PGDATABASE,
    # PGUSER, PGPASSWORD, PGSSLMODE, and other standard PG* variables.
    try:
        with psycopg.connect("", application_name="postgres_csv_export") as conn:
            conn.execute("SET TRANSACTION READ ONLY")
            for table, output_path in zip(tables, output_paths):
                print(f"Exporting {table.display_name} -> {output_path}")
                export_table(conn, table, output_path, args.geom_column)
            conn.commit()
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print(f"Done: exported {len(tables)} table(s) to {output_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
