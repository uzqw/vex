# License Information

## Project License

Vex is licensed under the **Apache License 2.0**.

See the [LICENSE](../LICENSE) file for the full license text.

## Third-Party Licenses

This project includes code from third-party sources with different licenses. All third-party licenses are compatible with Apache 2.0.

### Weaviate SIMD Code (BSD-3-Clause)

**What we use:** SIMD-optimized C source code for vector operations

**Files:**
- `internal/vector/c/dot_avx256_amd64.c`
- `internal/vector/c/dot_avx512_amd64.c`

**License:** BSD-3-Clause
**Copyright:** (c) 2020-2025, Weaviate B.V.
**Source:** https://github.com/weaviate/weaviate

**Why BSD-3 is compatible with Apache 2.0:**
- Both are permissive open source licenses
- BSD-3 code can be included in Apache 2.0 projects
- We maintain all required copyright notices and attributions

For the complete license text, see [THIRD_PARTY_LICENSES.md](../THIRD_PARTY_LICENSES.md).

## License Compliance

### What This Means for Users

✓ **You can:**
- Use Vex commercially
- Modify and distribute Vex
- Create derivative works
- Use the SIMD optimizations in your own projects

✓ **You must:**
- Include the Apache 2.0 license when distributing
- Provide attribution to both Vex and Weaviate
- Include copyright notices from both projects

### Attribution Requirements

When using or distributing Vex, please include:

1. **Apache 2.0 License** (main license)
2. **NOTICE file** (lists all attributions)
3. **THIRD_PARTY_LICENSES.md** (if redistributing source code)

### For Developers

When modifying the SIMD code:

1. **Keep copyright headers** in C files (`internal/vector/c/*.c`)
2. **Maintain attributions** in generated assembly files
3. **Update NOTICE** if adding new third-party dependencies
4. **Document license changes** in this file

## References

- **Apache License 2.0**: https://www.apache.org/licenses/LICENSE-2.0
- **BSD-3-Clause License**: https://opensource.org/licenses/BSD-3-Clause
- **Apache License Compatibility**: https://www.apache.org/legal/resolved.html
- **Weaviate Project**: https://github.com/weaviate/weaviate

## Questions?

For licensing questions:
- Check [Apache Foundation's Legal FAQ](https://www.apache.org/legal/)
- Review [BSD License Overview](https://opensource.org/licenses/BSD-3-Clause)
- Open an issue in the project repository

---

*Last updated: January 2026*
