FROM r-base:4.3.2

ARG DEPSILO_URL

RUN R -e "options(repos = c(CRAN = '${DEPSILO_URL}/cran/')); install.packages('jsonlite')"

RUN R -e "library(jsonlite); cat('cran: jsonlite', as.character(packageVersion('jsonlite')), 'OK\n')"
