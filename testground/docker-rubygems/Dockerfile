FROM ruby:3.2-alpine

ARG RUBYGEMS_MIRROR

RUN gem sources --clear-all \
    && gem sources --add "${RUBYGEMS_MIRROR}/" \
    && gem install --no-document rake -v 13.1.0

RUN ruby -e "require 'rake'; puts 'rubygems: rake ' + Rake::VERSION + ' OK'"
