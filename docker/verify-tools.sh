#!/bin/sh
set -eu

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for command in ffmpeg libreoffice pdfinfo pdftoppm pdftotext tesseract yt-dlp; do
    command -v "$command" >/dev/null
done
tesseract --list-langs | grep -qx eng

python3 - "$work/sample.pdf" <<'PY'
import sys

path = sys.argv[1]
content = b"BT\n/F1 18 Tf\n72 720 Td\n(OpenLia PDF) Tj\nET\n"
objects = [
    b"<< /Type /Catalog /Pages 2 0 R >>",
    b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
    b"<< /Length " + str(len(content)).encode() + b" >>\nstream\n" + content + b"endstream",
    b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
]
data = b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"
offsets = [0]
for number, obj in enumerate(objects, 1):
    offsets.append(len(data))
    data += f"{number} 0 obj\n".encode() + obj + b"\nendobj\n"
xref = len(data)
data += b"xref\n0 6\n0000000000 65535 f \n"
for offset in offsets[1:]:
    data += f"{offset:010d} 00000 n \n".encode()
data += b"trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n"
data += str(xref).encode() + b"\n%%EOF\n"
with open(path, "wb") as output:
    output.write(data)
PY
pdftotext "$work/sample.pdf" "$work/sample.txt"
grep -Fqx 'OpenLia PDF' "$work/sample.txt"
pdftoppm -png -singlefile "$work/sample.pdf" "$work/sample" >/dev/null
tesseract "$work/sample.png" "$work/ocr" >/dev/null 2>&1
grep -Fq 'OpenLia PDF' "$work/ocr.txt"

python3 - "$work/sample.docx" <<'PY'
import sys
import zipfile

document = b'''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>OpenLia Office</w:t></w:r></w:p></w:body>
</w:document>'''
content_types = b'''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxml-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>'''
relationships = b'''<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>'''
with zipfile.ZipFile(sys.argv[1], "w") as output:
    output.writestr("[Content_Types].xml", content_types)
    output.writestr("_rels/.rels", relationships)
    output.writestr("word/document.xml", document)
PY
libreoffice --headless --convert-to txt:Text --outdir "$work" "$work/sample.docx" >/dev/null
grep -Fq 'OpenLia Office' "$work/sample.txt"

printf '%s\n' 'name,value' 'OpenLia,1' >"$work/sample.csv"
libreoffice --headless --convert-to xlsx --outdir "$work" "$work/sample.csv" >/dev/null
test -s "$work/sample.xlsx"

ffmpeg -version >/dev/null
yt-dlp --version >/dev/null
