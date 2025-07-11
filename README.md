# backup-xd


# on edit

```bash
go build -o backup-xd main.go
```

# config file

```bash
cat ~/.local/bin/backup-manager-config.json
```

# Postgres Restore

# 1. Drop and recreate database (if needed)
dropdb gator
createdb gator

# 2. Restore from backup
psql -h localhost -U postgres -d gator -f ./backups/postgres/gator_123456.sql

# 3. Or with environment variables
source ~/.backup-env
psql -h $PGHOST -U $PGUSER -d gator -f ./backups/postgres/gator_123456.sql

# MongoDB Restore

# 1. Restore to same database (with --drop to replace existing data)
source ~/.backup-env
mongorestore --uri="$PROJECT_MANAGER_MONGODB_URI" --drop ./backups/mongodb/project-manager_123456/

# 2. Or restore to different database
mongorestore --uri="mongodb+srv://user:pass@cluster/NEW_DB_NAME" ./backups/mongodb/project-manager_123456/project-manager/