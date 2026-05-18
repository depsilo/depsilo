FROM r-base:4.3.2

ARG CRAN_MIRROR

RUN R -e "options(repos = c(CRAN = '${CRAN_MIRROR}/')); install.packages('jsonlite')"

RUN R -e "library(jsonlite); cat('cran: jsonlite', as.character(packageVersion('jsonlite')), 'OK\n')"
