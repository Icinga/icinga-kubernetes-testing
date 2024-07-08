USE `testing`;

CREATE TABLE `pod`
(
    `uuid`      BINARY(16)   NOT NULL,
    `namespace` VARCHAR(255) NOT NULL,
    `name`      VARCHAR(255) NOT NULL,
    PRIMARY KEY (`uuid`)
);

CREATE TABLE `pod_test`
(
    `pod_uuid` BINARY(16) NOT NULL,
    `test`  VARCHAR(255)        NOT NULL,
    PRIMARY KEY (`pod_uuid`, `test`),
    FOREIGN KEY (`pod_uuid`) REFERENCES `pod` (`uuid`)
);

GRANT SELECT ON `testing`.pod TO 'testing'@'%';
GRANT SELECT ON `testing`.pod_test TO 'testing'@'%';
