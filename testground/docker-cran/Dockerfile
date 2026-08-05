FROM r-base:4.3.2@sha256:4bdf68161b2ed65fa6ca8285d1dc445ceb8100e94f503c56f59d2cfc453c7cdd

ARG DEPSILO_URL

RUN R -e "options(repos = c(CRAN = '${DEPSILO_URL}/cran/')); install.packages('jsonlite')"

RUN R -e "library(jsonlite); cat('cran: jsonlite', as.character(packageVersion('jsonlite')), 'OK\n')"
