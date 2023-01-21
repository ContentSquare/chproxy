CREATE TABLE IF NOT EXISTS tutorial.views_distributed AS tutorial.views
ENGINE = Distributed('clickhouse_cluster', tutorial, views, uid);