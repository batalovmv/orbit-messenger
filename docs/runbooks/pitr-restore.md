# PITR Restore Runbook

> **Область применения**: PostgreSQL 16 с WAL-G архивацией на Cloudflare R2  
> **Время выполнения**: ~15–30 минут в зависимости от размера БД и объёма WAL

---

## Prerequisites

- WAL-G v3.0.3 установлен локально или доступен внутри контейнера
- Доступ к R2 (переменные `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`)
- PostgreSQL 16 CLI (`psql`, `pg_ctl`, `pg_isready`)
- Достаточно свободного места на диске (≥ размер БД × 2)

Экспортируй переменные окружения перед выполнением любых команд:

```bash
source /etc/wal-g.env.sh
# или вручную:
export AWS_ENDPOINT=<R2_ENDPOINT>
export AWS_ACCESS_KEY_ID=<key>
export AWS_SECRET_ACCESS_KEY=<secret>
export AWS_S3_FORCE_PATH_STYLE=true
export AWS_REGION=auto
export WALG_S3_PREFIX=s3://<R2_BUCKET>/wal-g
export PGHOST=/var/run/postgresql
```

---

## 1. Просмотр доступных бэкапов

```bash
wal-g backup-list
```

Пример вывода:
```
name                          modified             wal_segment_backup_start
base_000000010000000000000002  2026-04-20T10:00:00Z 000000010000000000000002
base_000000010000000000000005  2026-04-21T10:00:00Z 000000010000000000000005
```

Для детальной информации:
```bash
wal-g backup-list DETAIL
```

---

## 2. Восстановление последнего бэкапа (без PITR)

```bash
# 1. Остановить Postgres
pg_ctl stop -D $PGDATA -m fast

# 2. Очистить data directory (оставить только pg_wal если нужен replay)
rm -rf $PGDATA/*

# 3. Восстановить базовый бэкап
wal-g backup-fetch $PGDATA LATEST

# 4. Создать сигнальный файл для режима recovery
touch $PGDATA/recovery.signal

# 5. Запустить Postgres — он воспроизведёт WAL и выйдет в online
pg_ctl start -D $PGDATA
```

---

## 3. PITR — восстановление до конкретного момента времени

```bash
# 1. Остановить Postgres
pg_ctl stop -D $PGDATA -m fast

# 2. Очистить data directory
rm -rf $PGDATA/*

# 3. Восстановить базовый бэкап (можно указать конкретный или LATEST)
wal-g backup-fetch $PGDATA LATEST

# 4. Добавить recovery_target_time в postgresql.conf
cat >> $PGDATA/postgresql.conf <<EOF
restore_command = 'source /etc/wal-g.env.sh && wal-g wal-fetch %f %p'
recovery_target_time = '2026-04-21 09:30:00 UTC'
recovery_target_action = promote
EOF

# 5. Создать сигнальный файл
touch $PGDATA/recovery.signal

# 6. Запустить Postgres
pg_ctl start -D $PGDATA

# 7. Следить за логами — должна быть строка:
# "recovery stopping before commit of transaction ..., time 2026-04-21 09:30:xx UTC"
tail -f $PGDATA/log/postgresql.log | grep -E "recovery|PITR|promote"
```

> ⚠️ Время в `recovery_target_time` должно быть **после** момента создания базового бэкапа и **до** нужного события.

---

## 4. Локальное тестирование через Docker Compose

```bash
# 1. Запустить только postgres с переменными R2
docker compose up -d postgres

# 2. Войти в контейнер
docker compose exec postgres bash

# 3. Внутри контейнера выполнить восстановление
source /etc/wal-g.env.sh
wal-g backup-list

# 4. Остановить postgres внутри контейнера и восстановить
pg_ctl stop -D $PGDATA -m fast
rm -rf $PGDATA/*
wal-g backup-fetch $PGDATA LATEST
touch $PGDATA/recovery.signal
pg_ctl start -D $PGDATA
```

Или через временный контейнер для изоляции:
```bash
docker run --rm -it \
  -e R2_ENDPOINT=$R2_ENDPOINT \
  -e R2_ACCESS_KEY_ID=$R2_ACCESS_KEY_ID \
  -e R2_SECRET_ACCESS_KEY=$R2_SECRET_ACCESS_KEY \
  -e R2_BUCKET=$R2_BUCKET \
  -v /tmp/pgdata-restore:/var/lib/postgresql/data \
  orbit-postgres bash
```

---

## 5. Верификация после восстановления

```bash
# Postgres готов к соединениям?
pg_isready -U orbit -d orbit
# ожидаемый ответ: /var/run/postgresql:5432 - accepting connections

# Количество строк в ключевых таблицах
psql -U orbit -d orbit -c "SELECT COUNT(*) FROM users;"
psql -U orbit -d orbit -c "SELECT COUNT(*) FROM messages;"
psql -U orbit -d orbit -c "SELECT COUNT(*) FROM chats;"

# Проверить последнюю транзакцию (для PITR)
psql -U orbit -d orbit -c "SELECT MAX(created_at) FROM messages;"

# Проверить целостность
psql -U orbit -d orbit -c "SELECT schemaname, tablename, n_live_tup FROM pg_stat_user_tables ORDER BY n_live_tup DESC LIMIT 10;"
```

---

## Rollback / Отмена восстановления

Если что-то пошло не так:
1. Остановить Postgres: `pg_ctl stop -D $PGDATA -m fast`
2. Восстановить из другого бэкапа (указать конкретное имя вместо `LATEST`)
3. Или поднять prod snapshot из R2 в другую директорию для сравнения

---

## Регулярный cron-бэкап

Скрипт `scripts/postgres/backup.sh` должен запускаться ежедневно.  
В docker-compose предусмотрен сервис `backup` (см. compose) с переменной `BACKUP_CRON`.
