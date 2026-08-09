#!/usr/bin/env python3
from setuptools import setup, find_packages

with open("README.md", "r", encoding="utf-8") as fh:
    long_description = fh.read()

setup(
    name="etcd-extract",
    version="0.1.0",
    author="Your Name",
    description="Extract Kubernetes objects from etcd database files",
    long_description=long_description,
    long_description_content_type="text/markdown",
    py_modules=["etcd_extract"],
    python_requires=">=3.7",
    install_requires=[
        "bolt-python>=0.1.0",
        "PyYAML>=6.0",
    ],
    entry_points={
        "console_scripts": [
            "etcd-extract=etcd_extract:main",
        ],
    },
    classifiers=[
        "Development Status :: 3 - Alpha",
        "Intended Audience :: System Administrators",
        "Topic :: System :: Systems Administration",
        "License :: OSI Approved :: MIT License",
        "Programming Language :: Python :: 3",
        "Programming Language :: Python :: 3.7",
        "Programming Language :: Python :: 3.8",
        "Programming Language :: Python :: 3.9",
        "Programming Language :: Python :: 3.10",
        "Programming Language :: Python :: 3.11",
    ],
)
