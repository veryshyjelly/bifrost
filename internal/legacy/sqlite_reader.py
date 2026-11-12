"""Read a frozen legacy SQLite view; never execute archived application code."""
import hashlib
import json
import sqlite3
import sys
from pathlib import Path

TABLES = {
    "kv": ("kind", "id", "value"),
    "jobs": ("id", "dedup", "stream", "type", "payload", "state", "attempts", "due", "error", "created"),
    "effects": ("id", "state", "result"),
    "messages": ("wa_chat", "wa_id", "tg_chat", "tg_id", "bot", "direction", "media"),
    "aliases": ("alias", "canonical"),
    "telegram_messages": ("seq", "scope", "chat", "message", "bot", "thread", "kind", "date", "expires", "deleted", "error"),
}


def encoded(value):
    return (json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False) + "\n").encode("utf-8")


def emit(value):
    sys.stdout.buffer.write(encoded(value))


def authorize(action, first, second, database, trigger):
    allowed = {sqlite3.SQLITE_SELECT, sqlite3.SQLITE_READ, sqlite3.SQLITE_FUNCTION,
               sqlite3.SQLITE_TRANSACTION, sqlite3.SQLITE_PRAGMA}
    if action not in allowed:
        return sqlite3.SQLITE_DENY
    if action == sqlite3.SQLITE_FUNCTION and (second or first or "").lower() in {
            "load_extension", "writefile", "readfile", "eval", "fts3_tokenizer"}:
        return sqlite3.SQLITE_DENY
    if action == sqlite3.SQLITE_PRAGMA and (first or "").lower() not in {
            "table_info", "integrity_check", "user_version"}:
        return sqlite3.SQLITE_DENY
    return sqlite3.SQLITE_OK


def main():
    path, maximum, maximum_record = Path(sys.argv[1]).resolve(strict=True), int(sys.argv[2]), int(sys.argv[3])
    # Never immutable=1: a committed WAL may contain the newest source state.
    connection = sqlite3.connect(path.as_uri() + "?mode=ro", uri=True, isolation_level=None, timeout=5)
    try:
        connection.setlimit(sqlite3.SQLITE_LIMIT_LENGTH, maximum_record)
        connection.setlimit(sqlite3.SQLITE_LIMIT_ATTACHED, 0)
        connection.execute("PRAGMA query_only=ON")
        connection.execute("PRAGMA trusted_schema=OFF")
        connection.execute("PRAGMA cell_size_check=ON")
        connection.set_authorizer(authorize)
        connection.execute("BEGIN")
        schema = connection.execute("SELECT name,type,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").fetchall()
        objects = {name: (kind, sql or "") for name, kind, sql in schema if kind != "index"}
        if set(objects) != set(TABLES) or any(kind != "table" or "VIRTUAL TABLE" in sql.upper()
                                              for kind, sql in objects.values()):
            raise ValueError("schema")
        for table, columns in TABLES.items():
            actual = connection.execute(f'PRAGMA table_info("{table}")').fetchall()
            if tuple(row[1] for row in actual) != columns:
                raise ValueError("columns")
        if connection.execute("PRAGMA integrity_check").fetchall() != [("ok",)]:
            raise ValueError("integrity")
        counts = {table: connection.execute(f'SELECT count(*) FROM "{table}"').fetchone()[0] for table in TABLES}
        if sum(counts.values()) > maximum:
            raise ValueError("row limit")
        emit({"record": "header", "format": "bifrost-legacy-sqlite", "version": 1,
              "sqliteVersion": sqlite3.sqlite_version,
              "userVersion": connection.execute("PRAGMA user_version").fetchone()[0], "counts": counts})
        digest = hashlib.sha256()
        for table, columns in TABLES.items():
            fields = ",".join('"' + column + '"' for column in columns)
            ordering = {"kv": "kind,id", "jobs": "id", "effects": "id", "messages": "rowid",
                        "aliases": "alias", "telegram_messages": "seq"}[table]
            for row in connection.execute(f'SELECT {fields} FROM "{table}" ORDER BY {ordering}'):
                if any(value is not None and not isinstance(value, (str, int)) for value in row):
                    raise ValueError("storage type")
                record = encoded({"record": "row", "table": table, "values": dict(zip(columns, row))})
                if len(record) > maximum_record:
                    raise ValueError("record limit")
                digest.update(record)
                sys.stdout.buffer.write(record)
        emit({"record": "end", "counts": counts, "digest": digest.hexdigest()})
        sys.stdout.buffer.flush()
        connection.execute("ROLLBACK")
    finally:
        connection.close()


if __name__ == "__main__":
    try:
        main()
    except Exception:
        sys.stderr.write("legacy SQLite snapshot validation failed\n")
        sys.exit(1)
