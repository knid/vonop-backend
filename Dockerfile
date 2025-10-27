FROM --platform=linux/amd64 debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    ANDROID_SDK_ROOT=/opt/android-sdk \
    PATH=/usr/local/flutter/bin:/usr/local/flutter/bin/cache/dart-sdk/bin:$ANDROID_SDK_ROOT/cmdline-tools/latest/bin:$ANDROID_SDK_ROOT/platform-tools:$PATH

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash curl file git unzip wget zip ca-certificates \
    openjdk-17-jdk \
    python3 python3-pip make \
    python3-psycopg2 python3-jinja2 \
    && rm -rf /var/lib/apt/lists/*

RUN wget https://go.dev/dl/go1.25.1.linux-amd64.tar.gz -O /tmp/go.tar.gz && \
    rm -rf /usr/local/go && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    ln -s /usr/local/go/bin/go /usr/bin/go && \
    ln -s /usr/local/go/bin/gofmt /usr/bin/gofmt && \
    rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
ENV PATH="/root/go/bin:$PATH"

RUN mkdir -p $ANDROID_SDK_ROOT/cmdline-tools && \
    wget -q https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip -O /tmp/cmdline-tools.zip && \
    unzip /tmp/cmdline-tools.zip -d $ANDROID_SDK_ROOT/cmdline-tools && \
    mv $ANDROID_SDK_ROOT/cmdline-tools/cmdline-tools $ANDROID_SDK_ROOT/cmdline-tools/latest && \
    chmod +x $ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager && \
    rm /tmp/cmdline-tools.zip

ENV PATH="$ANDROID_SDK_ROOT/cmdline-tools/latest/bin:$ANDROID_SDK_ROOT/platform-tools:$PATH"

# Test
RUN ls -l $ANDROID_SDK_ROOT/cmdline-tools/latest/bin && \
    sdkmanager --version

# Lisans ve paketler
RUN yes | sdkmanager --licenses && \
    sdkmanager --install "platform-tools" "platforms;android-36" "build-tools;34.0.0"

# Flutter kurulumu
RUN git clone https://github.com/flutter/flutter.git /usr/local/flutter && \
    flutter config --android-sdk $ANDROID_SDK_ROOT

# Flutter doctor (cache indirir, ilk build hızlanır)
RUN flutter doctor -v

# Çalışma dizini
WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go build -o wemob-api 

CMD ["./wemob-api"]