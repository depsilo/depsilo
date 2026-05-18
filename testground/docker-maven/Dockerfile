FROM maven:3.9-eclipse-temurin-17

ARG MAVEN_MIRROR

RUN mkdir -p /root/.m2 && printf '<?xml version="1.0"?>\n<settings>\n  <mirrors>\n    <mirror>\n      <id>depsilo</id>\n      <mirrorOf>*</mirrorOf>\n      <url>%s</url>\n    </mirror>\n  </mirrors>\n</settings>\n' "${MAVEN_MIRROR}" > /root/.m2/settings.xml

RUN mvn -B dependency:get -Dartifact=com.google.guava:guava:32.1.3-jre

RUN echo "maven: guava OK"
