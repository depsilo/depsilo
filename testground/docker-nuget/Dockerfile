FROM mcr.microsoft.com/dotnet/sdk:8.0-alpine@sha256:5f12aa62868b69dcb41de9cd7f8759822f7d1f56c3b31908048ad65df0981e67

ARG DEPSILO_URL

WORKDIR /test
RUN dotnet new console -o app --no-restore \
    && cd app \
    && dotnet nuget add source "${DEPSILO_URL}/nuget/v3/index.json" -n depsilo \
    && dotnet nuget remove source nuget.org 2>/dev/null || true

RUN cd /test/app && dotnet add package Newtonsoft.Json --version 13.0.3

RUN cd /test/app && dotnet build -v q && echo "nuget: Newtonsoft.Json OK"
