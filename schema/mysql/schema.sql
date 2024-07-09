CREATE TABLE test (
    uuid binary(16) NOT NULL,
    name varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    namespace varchar(63) COLLATE utf8mb4_unicode_ci NOT NULL,
    uid varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,

    PRIMARY KEY (uuid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
