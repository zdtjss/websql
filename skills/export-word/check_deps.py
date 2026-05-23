import sys
try:
    import matplotlib
    print('matplotlib OK')
    import numpy
    print('numpy OK')
    from docx import Document
    print('python-docx OK')
    print('All dependencies OK')
except Exception as e:
    print(f'Error: {e}')
    sys.exit(1)
