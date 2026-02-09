"""
TDTP Framework - Python bindings
Express data integration library
"""

from setuptools import setup, find_packages
from pathlib import Path

# Read README
readme_file = Path(__file__).parent / 'README.md'
long_description = ''
if readme_file.exists():
    long_description = readme_file.read_text(encoding='utf-8')

setup(
    name='tdtp-framework',
    version='1.6.0',
    author='Ruslan',
    author_email='ruslano69@gmail.com',
    description='TDTP Framework - Express data integration between databases',
    long_description=long_description,
    long_description_content_type='text/markdown',
    url='https://github.com/ruslano69/tdtp-framework',
    project_urls={
        'Bug Reports': 'https://github.com/ruslano69/tdtp-framework/issues',
        'Source': 'https://github.com/ruslano69/tdtp-framework',
    },

    packages=find_packages(),
    package_data={
        'tdtp': [
            '*.so',      # Linux shared library
            '*.dylib',   # macOS shared library
            '*.dll',     # Windows shared library
        ],
    },

    python_requires='>=3.7',
    install_requires=[],  # No dependencies! Only stdlib

    extras_require={
        'dev': [
            'pytest>=6.0',
            'pytest-cov>=2.0',
        ],
        'pandas': [
            'pandas>=1.0.0',
        ],
    },

    classifiers=[
        'Development Status :: 4 - Beta',
        'Intended Audience :: Developers',
        'Topic :: Database',
        'Topic :: Software Development :: Libraries :: Python Modules',
        'License :: OSI Approved :: MIT License',
        'Programming Language :: Python :: 3',
        'Programming Language :: Python :: 3.7',
        'Programming Language :: Python :: 3.8',
        'Programming Language :: Python :: 3.9',
        'Programming Language :: Python :: 3.10',
        'Programming Language :: Python :: 3.11',
        'Programming Language :: Go',
    ],

    keywords='database integration etl data-transfer tdtp xml',
)
