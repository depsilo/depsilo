FROM composer:2

ARG DEPSILO_URL

ENV COMPOSER_ALLOW_SUPERUSER=1
WORKDIR /test

# Override the default entrypoint to allow RUN commands.
# secure-http must be disabled because Depsilo is served over plain HTTP
# from the docker0 gateway; composer otherwise refuses non-https sources.
RUN composer config -g secure-http false \
    && composer config -g repo.packagist composer "${DEPSILO_URL}/composer" \
    && composer init --name "test/test" --no-interaction \
    && composer require --no-progress --no-interaction monolog/monolog:^3.0

RUN composer show monolog/monolog | head -1 && echo "composer: monolog OK"
