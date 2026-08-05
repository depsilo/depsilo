FROM ruby:3.2-alpine@sha256:d206c25708a44df6a7ce22213ee5da9fc0a9f7b31ce884429ba758db48abdc62

ARG DEPSILO_URL

RUN gem sources --clear-all \
    && gem sources --add "${DEPSILO_URL}/rubygems/" \
    && gem install --no-document rake -v 13.1.0

RUN ruby -e "require 'rake'; puts 'rubygems: rake ' + Rake::VERSION + ' OK'"
