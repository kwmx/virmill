"""Relocate a bounded subset of Markdown for installed development documents.

This module reads no files. The packager supplies the reviewed source inventory
and selected source-to-installed paths. Source-only references retain their
meaning without suggesting that omitted implementation or evidence is installed.
"""
import posixpath
import re
import string
from pathlib import PurePosixPath
from urllib.parse import quote, unquote, urlsplit


MAX_DOCUMENT_BYTES = 8 * 1024 * 1024
MAX_OUTPUT_BYTES = 16 * 1024 * 1024
MAX_PATHS = 100000
MAX_LINKS = 20000
MAX_PATH_BYTES = 4096
MAX_INVENTORY_BYTES = 32 * 1024 * 1024


def _canonical(path):
    if (not isinstance(path, str) or not path or len(path.encode('utf-8')) > MAX_PATH_BYTES
            or any(ord(c) < 32 or ord(c) == 127 for c in path) or '\\' in path
            or path.startswith('/') or str(PurePosixPath(path)) != path
            or '..' in PurePosixPath(path).parts or path == '.'):
        raise ValueError('Noncanonical documentation inventory path')
    return path


def _inventory(installed_by_source, tracked_sources):
    if len(installed_by_source) > MAX_PATHS or len(tracked_sources) > MAX_PATHS:
        raise ValueError('Documentation inventory exceeds path limit')
    known, directory_targets = set(), {}
    total = 0
    for source in sorted(tracked_sources):
        total += len(_canonical(source).encode('utf-8'))
        known.add(source)
        known.update(str(p) for p in PurePosixPath(source).parents if str(p) != '.')
    targets = set()
    for source, target in sorted(installed_by_source.items()):
        total += len(_canonical(source).encode('utf-8')) + len(_canonical(target).encode('utf-8'))
        if source not in tracked_sources or target in targets:
            raise ValueError('Documentation mapping is untracked or collides')
        targets.add(target)
        source_parent, target_parent = PurePosixPath(source).parent, PurePosixPath(target).parent
        while str(source_parent) != '.':
            if str(target_parent) == '.':
                raise ValueError('Documentation mapping has insufficient installed hierarchy')
            directory_targets.setdefault(str(source_parent), set()).add(str(target_parent))
            source_parent, target_parent = source_parent.parent, target_parent.parent
    if total > MAX_INVENTORY_BYTES:
        raise ValueError('Documentation inventory exceeds byte limit')
    mapping = dict(installed_by_source)
    for source, options in directory_targets.items():
        if len(options) == 1 and source not in mapping:
            mapping[source] = next(iter(options))
    return known, mapping


def _blank(text):
    return ''.join('\n' if c == '\n' else ' ' for c in text)


def _mask_inline(text):
    # Equal-length backtick runs pair only within a paragraph. Different-length
    # runs inside an inline code span are literal; their contents are untouched.
    pieces = []
    for paragraph in re.split(r'(\n[ \t]*\n)', text):
        runs = list(re.finditer(r'`+', paragraph))
        if len(runs) > MAX_LINKS * 2:
            raise ValueError('Documentation code-span limit exceeded')
        following, previous = {}, {}
        for index in range(len(runs) - 1, -1, -1):
            length = len(runs[index][0])
            following[index] = previous.get(length)
            previous[length] = index
        cursor, index, parts = 0, 0, []
        while index < len(runs):
            close = following[index]
            if close is None:
                index += 1
                continue
            start, end = runs[index].start(), runs[close].end()
            parts.extend((paragraph[cursor:start], _blank(paragraph[start:end])))
            cursor, index = end, close + 1
        parts.append(paragraph[cursor:])
        pieces.append(''.join(parts))
    return ''.join(pieces)


def _mask_code(text):
    result, ordinary, fence = [], [], None
    for line in text.splitlines(keepends=True):
        match = re.match(r'^ {0,3}(`{3,}|~{3,})([^\n]*)', line)
        if fence is not None:
            if match and match[1][0] == fence[0] and len(match[1]) >= len(fence) and not match[2].strip():
                fence = None
            result.append(_blank(line))
        elif match:
            result.append(_mask_inline(''.join(ordinary)))
            ordinary = []
            fence = match[1]
            if fence[0] == '`' and '`' in match[2]:
                raise ValueError('Unsupported Markdown fence info')
            result.append(_blank(line))
        else:
            ordinary.append(line)
    if fence is not None:
        raise ValueError('Unclosed Markdown fence')
    result.append(_mask_inline(''.join(ordinary)))
    return ''.join(result)


def _label_ends(text):
    stack, result, cursor = [], {}, 0
    while cursor < len(text):
        if text[cursor] == '\\':
            cursor += 2
            continue
        if text[cursor] == '[':
            stack.append(cursor)
            if len(stack) > MAX_LINKS:
                raise ValueError('Documentation bracket nesting limit exceeded')
        elif text[cursor] == ']' and stack:
            result[stack.pop()] = cursor
        cursor += 1
    return result


def _destination(text, start):
    cursor = start
    while cursor < len(text) and text[cursor] in ' \t':
        cursor += 1
    angle = cursor < len(text) and text[cursor] == '<'
    token_start = cursor + 1 if angle else cursor
    cursor = token_start
    depth = 0
    while cursor < len(text):
        char = text[cursor]
        if char == '\\' and cursor + 1 < len(text) and text[cursor + 1] in string.punctuation:
            cursor += 2
            continue
        if char in '\r\n':
            raise ValueError('Multiline Markdown destination is unsupported')
        if angle:
            if char == '>':
                break
            if char == '<':
                raise ValueError('Nested Markdown destination angle')
        elif char == '(':
            depth += 1
        elif char == ')':
            if depth == 0:
                break
            depth -= 1
        elif char.isspace():
            break
        cursor += 1
    token_end = cursor
    if angle:
        if cursor == len(text) or text[cursor] != '>':
            raise ValueError('Unclosed Markdown destination angle')
        cursor += 1
    while cursor < len(text) and text[cursor] in ' \t':
        cursor += 1
    if cursor == len(text) or text[cursor] != ')' or depth:
        raise ValueError('Malformed destination or unsupported Markdown link title')
    destination = re.sub(r'\\([' + re.escape(string.punctuation) + r'])', r'\1', text[token_start:token_end])
    return destination, token_start, token_end, cursor + 1


def _resolve(destination, source_path, known, mapping):
    if len(destination.encode('utf-8')) > MAX_PATH_BYTES or any(ord(c) < 32 or ord(c) == 127 for c in destination):
        raise ValueError('Invalid or oversized Markdown destination')
    parts = urlsplit(destination)
    if parts.scheme:
        if ((parts.scheme in ('http', 'https') and parts.netloc)
                or (parts.scheme == 'mailto' and parts.path)):
            return None
        raise ValueError('Unsupported documentation URI scheme')
    if parts.netloc or parts.path.startswith('/'):
        raise ValueError('Absolute documentation destination is not portable')
    if parts.query:
        raise ValueError('Local documentation query is unsupported')
    if not parts.path:
        return None
    if re.search(r'%(?![0-9a-fA-F]{2})', parts.path):
        raise ValueError('Malformed documentation URL escape')
    path = unquote(parts.path, errors='strict')
    if '\\' in path or path.startswith('/') or any(ord(c) < 32 or ord(c) == 127 for c in path):
        raise ValueError('Unsafe decoded documentation path')
    target = posixpath.normpath(posixpath.join(posixpath.dirname(source_path), path))
    if target in ('.', '..') or target.startswith('../'):
        raise ValueError('Documentation destination escapes the source inventory')
    line = None
    if target not in known:
        match = re.fullmatch(r'(.+):([1-9][0-9]{0,9})', target)
        if match and match[1] in known and int(match[2]) <= 2147483647:
            target, line = match[1], match[2]
    if target not in known:
        raise ValueError('Unresolved documentation destination: ' + target)
    return target, mapping.get(target), line, parts.fragment


def _literal(text):
    width = max((len(m[0]) for m in re.finditer(r'`+', text)), default=0) + 1
    delimiter = '`' * width
    padding = ' ' if text.startswith(('`', ' ')) or text.endswith(('`', ' ')) else ''
    return delimiter + padding + text + padding + delimiter


def rewrite_markdown(source_path: str, installed_path: str, data: bytes,
                     installed_by_source: dict[str, str], tracked_sources: set[str]) -> bytes:
    """Return installed document bytes, or refuse an unresolved/unsupported link.

    Only inline links/images, HTTP(S)/mailto URIs and fragment-only links are
    supported. Reference/HTML links, link titles and ambiguous indented links
    are refused. No network, filesystem, build or package operation occurs here.
    """
    _canonical(source_path)
    _canonical(installed_path)
    if not isinstance(data, bytes) or len(data) > MAX_DOCUMENT_BYTES:
        raise ValueError('Documentation exceeds byte limit')
    known, mapping = _inventory(installed_by_source, tracked_sources)
    if source_path not in tracked_sources or installed_by_source.get(source_path) != installed_path:
        raise ValueError('Document origin does not match the installation mapping')
    text = data.decode('utf-8', errors='strict')
    masked = _mask_code(text)
    if (re.search(r'(?im)^ {0,3}\[[^\]\n]+\]:', masked)
            or re.search(r'(?is)<[a-z][^<>]*\b(?:href|src)\s*=', masked)
            or any(re.match(r'^(?: {4}|\t)', original) and '](' in visible
                   for original, visible in zip(text.splitlines(), masked.splitlines()))):
        raise ValueError('Unsupported reference, HTML or indented Markdown link')
    label_ends = _label_ends(masked)
    pieces, cursor, scan, links = [], 0, 0, 0
    while scan < len(masked):
        start = masked.find('[', scan)
        if start < 0:
            break
        # Escaped brackets remain prose; their apparent link syntax is checked
        # below rather than silently interpreted as an installed dependency.
        before = start
        while before > 0 and masked[before - 1] == '\\':
            before -= 1
        backslashes = start - before
        if backslashes % 2:
            scan = start + 1
            continue
        close = label_ends.get(start)
        if close is None:
            scan = start + 1
            continue
        if masked[close + 1:close + 2] == '[':
            raise ValueError('Reference-style Markdown links are unsupported')
        if masked[close + 1:close + 2] != '(':
            scan = start + 1
            continue
        destination, token_start, token_end, end = _destination(text, close + 2)
        links += 1
        if links > MAX_LINKS:
            raise ValueError('Documentation link limit exceeded')
        resolved = _resolve(destination, source_path, known, mapping)
        image = start > 0 and masked[start - 1] == '!'
        link_start = start - 1 if image else start
        replacement = text[link_start:end]
        if resolved is not None:
            target, installed_target, line, fragment = resolved
            if installed_target is not None:
                relative = posixpath.relpath(installed_target, posixpath.dirname(installed_path))
                url = quote(relative, safe='/.-_~') + ('#' + fragment if fragment else '')
                replacement = text[link_start:token_start] + url + text[token_end:end]
                if line is not None:
                    replacement += ' (line ' + line + ')'
            else:
                reference = target + (':' + line if line is not None else '') + ('#' + fragment if fragment else '')
                replacement = text[start + 1:close] + ' (source checkout: ' + _literal(reference) + ')'
        pieces.extend((text[cursor:link_start], replacement))
        cursor, scan = end, end
    if masked.count('](') != links:
        raise ValueError('Unsupported or malformed inline Markdown link')
    pieces.append(text[cursor:])
    result = ''.join(pieces).encode('utf-8')
    if len(result) > MAX_OUTPUT_BYTES:
        raise ValueError('Relocated documentation exceeds byte limit')
    return result
