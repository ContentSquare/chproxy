CREATE TABLE IF NOT EXISTS tutorial.views (
    datetime DateTime,
    uid UInt64,
    page String,
    views UInt32
)ENGINE = GenerateRandom(1, 5, 3);