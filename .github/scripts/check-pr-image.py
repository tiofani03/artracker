import os
import re
import sys

def main():
    body = os.environ.get('PR_BODY', '')
    print('=== Checking PR description for PNG/WebP/JPG screenshot attachment ===')
    
    # Pattern matching markdown images, html img tags, or GitHub uploaded assets
    img_pattern = re.compile(
        r'(!\[.*?\]\(.*?\.(png|webp|jpg|jpeg)(\?.*)?\)|<img\s+[^>]*src=[\'"][^\'"]+\.(png|webp|jpg|jpeg)(\?.*)?[\'"]|https://github\.com/user-attachments/assets/[a-f0-9\-]+|https://user-images\.githubusercontent\.com/\S+)',
        re.IGNORECASE
    )
    
    has_valid_img = bool(img_pattern.search(body)) and 'url_gambar_kamu_disini' not in body
    has_svg = ('.svg' in body.lower())
    
    if not has_valid_img and has_svg:
        print('❌ ERROR: Format .svg tidak diperbolehkan sebagai screenshot bukti visual!')
        print('Gunakan screenshot gambar riil (.png, .webp, atau .jpg/.jpeg).')
        sys.exit(1)
        
    if has_valid_img:
        print('✅ Valid visual evidence (.png / .webp / .jpg) detected in PR description.')
        sys.exit(0)
    else:
        print('❌ ERROR: PR wajib melampirkan screenshot riil (.png, .webp, atau .jpg/.jpeg)!')
        print('Silakan upload gambar ke deskripsi PR (drag-and-drop file gambar), lalu rerun.')
        sys.exit(1)

if __name__ == '__main__':
    main()
