FROM composer:2

ARG COMPOSER_MIRROR

ENV COMPOSER_ALLOW_SUPERUSER=1
WORKDIR /test

# Override the default entrypoint to allow RUN commands
RUN composer config -g repo.packagist composer "${COMPOSER_MIRROR}" \
    && composer init --name "test/test" --no-interaction \
    && composer require --no-progress --no-interaction monolog/monolog:^3.0

RUN composer show monolog/monolog | head -1 && echo "composer: monolog OK"
