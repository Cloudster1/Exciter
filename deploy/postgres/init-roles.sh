#!/bin/sh
# Runs once, on first initialisation of the primary: exciter_app is bound by every RLS policy, exciter_admin is
# exempt by policy rather than by being superuser, and the foundation migration creates them only without a password.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
    CREATE ROLE exciter_app LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${APP_DB_PASSWORD:-exciter_app}';
    CREATE ROLE exciter_admin LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD '${ADMIN_DB_PASSWORD:-exciter_admin}';
    CREATE ROLE replicator WITH REPLICATION LOGIN PASSWORD '${REPLICATION_PASSWORD:-replicator}';

    GRANT CONNECT ON DATABASE "$POSTGRES_DB" TO exciter_app, exciter_admin;

    -- The pool watches pg_stat_wal_receiver to tell a caught-up standby from one whose receiver dropped.
    GRANT pg_read_all_stats TO exciter_app, exciter_admin;
SQL

# The streaming replica connects as replicator, and replication connections match no database entry.
cat >> "$PGDATA/pg_hba.conf" <<-HBA
	host replication replicator all scram-sha-256
HBA
