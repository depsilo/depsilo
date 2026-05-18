FROM continuumio/miniconda3

ARG CONDA_CHANNEL

# Point all channels through Depsilo. Tsinghua-style layout: <root>/pkgs/main and <root>/cloud/conda-forge.
RUN conda config --remove-key channels 2>/dev/null || true \
    && conda config --add channels "${CONDA_CHANNEL}/pkgs/main" \
    && conda config --set show_channel_urls yes \
    && conda config --set ssl_verify false

RUN conda install -y -n base requests=2.31 \
    && python -c "import requests; print('conda: requests', requests.__version__, 'OK')"
