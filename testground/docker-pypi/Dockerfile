FROM python:3.11-slim

ARG PIP_INDEX_URL
ARG PIP_TRUSTED_HOST

RUN pip install --no-cache-dir opencv-python-headless

# Verify installation
RUN python -c "import cv2; print('opencv', cv2.__version__, 'OK')"
