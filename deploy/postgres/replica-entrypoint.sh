#!/bin/sh
# Brings up a genuine streaming replica: on an empty data directory it takes a base backup from the primary and
# starts as a hot standby, so read routing and the read-your-writes fallback meet real replication.
set -e

# Root takes ownership of a fresh volume, as the stock entrypoint does, then postgres does the rest.
if [ "$(id -u)" = '0' ]; then
    mkdir -p "$PGDATA"
    chown -R postgres:postgres /var/lib/postgresql
    chmod 0750 "$PGDATA"
fi

if [ ! -s "$PGDATA/PG_VERSION" ]; then
    echo "replica: waiting for primary at $PRIMARY_HOST"
    until pg_isready -h "$PRIMARY_HOST" -U "$REPLICATION_USER" -q; do
        sleep 1
    done

    # The slot keeps WAL on the primary while this replica is offline. It is made idempotently, because a slot can
    # outlive the replica's volume and pg_basebackup --create-slot would then fail.
    echo "replica: ensuring replication slot $REPLICATION_SLOT"
    gosu postgres env PGPASSWORD="$REPLICATION_PASSWORD" psql \
        --host="$PRIMARY_HOST" --username="$REPLICATION_USER" --dbname=postgres \
        --no-password --quiet --tuples-only <<-SLOT
	SELECT pg_create_physical_replication_slot('$REPLICATION_SLOT')
	WHERE NOT EXISTS (
	    SELECT 1 FROM pg_replication_slots WHERE slot_name = '$REPLICATION_SLOT'
	);
	SLOT

    echo "replica: taking base backup from primary"
    rm -rf "${PGDATA:?}"/*
    # --write-recovery-conf points the standby back at the primary.
    gosu postgres env PGPASSWORD="$REPLICATION_PASSWORD" pg_basebackup \
        --host="$PRIMARY_HOST" \
        --username="$REPLICATION_USER" \
        --pgdata="$PGDATA" \
        --wal-method=stream \
        --slot="$REPLICATION_SLOT" \
        --write-recovery-conf \
        --progress --no-password
    chmod 0750 "$PGDATA"
    echo "replica: base backup complete"
fi

# The standby tells the primary which rows its reads still see, so the primary keeps them and replay never has to
# cancel a long read for a conflict with recovery (#435).
exec docker-entrypoint.sh postgres -c hot_standby_feedback=on
