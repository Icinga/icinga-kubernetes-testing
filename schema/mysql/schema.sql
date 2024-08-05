CREATE TABLE test (
    uuid binary(16) NOT NULL,
    name varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    namespace varchar(63) COLLATE utf8mb4_unicode_ci NOT NULL,
    uid varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    resource_version varchar(255) NOT NULL,
    resource_type varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    resource_name varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    description text COLLATE utf8mb4_unicode_ci NOT NULL,
    expected_pods int unsigned NOT NULL,
    created bigint unsigned NOT NULL,

    PRIMARY KEY (uuid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE test_pod (
    test_uuid binary(16) NOT NULL,
    kind varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    percentage int unsigned NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE template (
    id varchar(32) NOT NULL,
    name varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    created bigint unsigned NOT NULL,
    modified bigint unsigned,

    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE template_test (
    template_id varchar(32) NOT NULL,
    test_kind varchar(255) COLLATE utf8mb4_unicode_ci NOT NULL,
    total_replicas int unsigned NOT NULL,
    bad_replicas int unsigned NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
