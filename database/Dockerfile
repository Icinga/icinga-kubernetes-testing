FROM mariadb:latest

ENV MYSQL_USER=testing
ENV MYSQL_PASSWORD=testing
ENV MYSQL_DATABASE=testing
ENV MYSQL_ROOT_PASSWORD=kubernetes

COPY schema.sql /docker-entrypoint-initdb.d

EXPOSE 3306
