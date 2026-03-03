import postgres from "postgres";

// Module-level singleton — reused across requests in the same process
let _sql: ReturnType<typeof postgres> | null = null;

export function getDb(): ReturnType<typeof postgres> {
  if (!_sql) {
    _sql = postgres(process.env.DATABASE_URL!, { max: 3 });
  }
  return _sql;
}
