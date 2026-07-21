FROM mcr.microsoft.com/dotnet/sdk:8.0-alpine

ARG DEPSILO_URL

WORKDIR /test
RUN dotnet new console -o app --no-restore \
    && cd app \
    && dotnet nuget add source "${DEPSILO_URL}/nuget/v3/index.json" -n depsilo \
    && dotnet nuget remove source nuget.org 2>/dev/null || true

RUN cd /test/app && dotnet add package Newtonsoft.Json --version 13.0.3

RUN cd /test/app && dotnet build -v q && echo "nuget: Newtonsoft.Json OK"
