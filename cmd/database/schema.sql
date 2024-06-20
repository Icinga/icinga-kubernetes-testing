USE `testing`;

CREATE TABLE `pod`
(
    `uuid`      BINARY(16)   NOT NULL,
    `namespace` VARCHAR(255) NOT NULL,
    `name`      VARCHAR(255) NOT NULL,
    PRIMARY KEY (`uuid`)
);

CREATE TABLE `test`
(
    `id`   INT          NOT NULL AUTO_INCREMENT,
    `name` VARCHAR(255) NOT NULL,
    PRIMARY KEY (`id`)
);

CREATE TABLE `pod_test`
(
    `pod_uuid` BINARY(16) NOT NULL,
    `test_id`  INT        NOT NULL,
    PRIMARY KEY (`pod_uuid`, `test_id`),
    FOREIGN KEY (`pod_uuid`) REFERENCES `pod` (`uuid`),
    FOREIGN KEY (`test_id`) REFERENCES `test` (`id`)
);

GRANT SELECT ON `testing`.pod TO 'testing'@'%';
GRANT SELECT ON `testing`.test TO 'testing'@'%';
GRANT SELECT ON `testing`.pod_test TO 'testing'@'%';
