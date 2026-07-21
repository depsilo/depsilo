FROM ruby:3.2-alpine

ARG DEPSILO_URL

RUN gem sources --clear-all \
    && gem sources --add "${DEPSILO_URL}/rubygems/" \
    && gem install --no-document rake -v 13.1.0

RUN ruby -e "require 'rake'; puts 'rubygems: rake ' + Rake::VERSION + ' OK'"
