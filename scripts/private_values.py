"""Private test-environment values that must stay out of tracked files.

Values come from the ignored `.virmill-local/private-values.json`, plus the
current user's home directory. Callers only write or print the placeholders.
"""
import json
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
CONFIG = ROOT / '.virmill-local/private-values.json'
# A whole value is not part of a longer name, path component or number.
LEAD, TRAIL = r'(?<![A-Za-z0-9._-])', r'(?![A-Za-z0-9_])'

# Credentials are refused everywhere, whether or not a private list exists.
CREDENTIALS = [
    ('<redacted-github-token>', re.compile(r'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{20,})')),
    ('<redacted-aws-key>', re.compile(r'\bAKIA[0-9A-Z]{16}\b')),
    ('<redacted-slack-token>', re.compile(r'\bxox[abprs]-[0-9A-Za-z-]{10,}')),
    ('<redacted-private-key>', re.compile(r'-----BEGIN [A-Z ]*PRIVATE KEY-----\s+[A-Za-z0-9+/=]{40,}')),
    # Reserved example domains (RFC 2606/6761) hold deliberate rejection fixtures.
    ('<redacted-url-credentials>', re.compile(
        r'\b[a-z][a-z0-9+.-]*://[^\s/:@"\'<>]+:[^\s/@"\'<>]+@'
        r'(?!(?:[^\s/:@"\'<>]*\.)?(?:example\.(?:com|net|org)|example|invalid|test|localhost)(?=[/:\s"\'<>]|$))')),
]


def load(config=CONFIG, include_home=True, home=None):
    """Return ordered (placeholder, pattern) rules; the private list is optional."""
    rules = []
    if config.is_file():
        for item in json.loads(config.read_text())['values']:
            pattern = re.escape(item['match'])
            if item.get('wholeWord'):
                pattern = LEAD + pattern + TRAIL
            flags = re.IGNORECASE if item.get('ignoreCase') else 0
            rules.append((item['placeholder'], re.compile(pattern, flags)))
    home = str(Path.home() if home is None else home).rstrip('/')
    if include_home and home.count('/') >= 2:  # never '/' or a top-level directory
        rules.append(('<dev-home>', re.compile(LEAD + re.escape(home) + TRAIL)))
    return rules


def redact(text, rules):
    """Return text with private values and credentials replaced, and the count."""
    total = 0
    for placeholder, pattern in rules + CREDENTIALS:
        text, count = pattern.subn(placeholder, text)
        total += count
    return text, total


def findings(text, rules):
    """Yield (line number, placeholder) for each match, never the value itself."""
    for placeholder, pattern in rules + CREDENTIALS:
        for match in pattern.finditer(text):
            yield text.count('\n', 0, match.start()) + 1, placeholder
