	.text
	.file	"dot_avx256_amd64.c"
	.globl	dot_256                         # -- Begin function dot_256
	.p2align	4, 0x90
	.type	dot_256,@function
dot_256:                                # @dot_256
# %bb.0:
	pushq	%rbp
	movq	%rsp, %rbp
	andq	$-8, %rsp
	movq	(%rcx), %r9
	cmpl	$7, %r9d
	jg	.LBB0_9
# %bb.1:
	leal	-1(%r9), %r8d
	testb	$3, %r9b
	je	.LBB0_2
# %bb.3:
	movl	%r9d, %eax
	andl	$3, %eax
	vxorps	%xmm0, %xmm0, %xmm0
	xorl	%ecx, %ecx
	.p2align	4, 0x90
.LBB0_4:                                # =>This Inner Loop Header: Depth=1
	vmovss	(%rdi), %xmm1                   # xmm1 = mem[0],zero,zero,zero
	vfmadd231ss	(%rsi), %xmm1, %xmm0    # xmm0 = (xmm1 * mem) + xmm0
	addq	$4, %rdi
	addq	$4, %rsi
	addq	$1, %rcx
	cmpl	%ecx, %eax
	jne	.LBB0_4
# %bb.5:
	subl	%ecx, %r9d
	cmpl	$3, %r8d
	jae	.LBB0_7
.LBB0_31:
	vmovss	%xmm0, (%rdx)
	movq	%rbp, %rsp
	popq	%rbp
	retq
.LBB0_9:
	vxorps	%xmm0, %xmm0, %xmm0
	cmpl	$32, %r9d
	jb	.LBB0_10
# %bb.11:
	leal	-32(%r9), %ecx
	testb	$32, %cl
	jne	.LBB0_12
# %bb.13:
	vmovups	(%rdi), %ymm3
	vmovups	32(%rdi), %ymm2
	vmovups	64(%rdi), %ymm1
	vmovups	96(%rdi), %ymm0
	vxorps	%xmm4, %xmm4, %xmm4
	vfmadd132ps	(%rsi), %ymm4, %ymm3    # ymm3 = (ymm3 * mem) + ymm4
	vfmadd132ps	32(%rsi), %ymm4, %ymm2  # ymm2 = (ymm2 * mem) + ymm4
	vfmadd132ps	64(%rsi), %ymm4, %ymm1  # ymm1 = (ymm1 * mem) + ymm4
	vfmadd132ps	96(%rsi), %ymm4, %ymm0  # ymm0 = (ymm0 * mem) + ymm4
	subq	$-128, %rdi
	subq	$-128, %rsi
	movl	%ecx, %r9d
	cmpl	$32, %ecx
	jae	.LBB0_20
	jmp	.LBB0_15
.LBB0_10:
	vxorps	%xmm1, %xmm1, %xmm1
	vxorps	%xmm2, %xmm2, %xmm2
	vxorps	%xmm3, %xmm3, %xmm3
	jmp	.LBB0_16
.LBB0_2:
	vxorps	%xmm0, %xmm0, %xmm0
	cmpl	$3, %r8d
	jb	.LBB0_31
.LBB0_7:
	movl	%r9d, %eax
	xorl	%ecx, %ecx
	.p2align	4, 0x90
.LBB0_8:                                # =>This Inner Loop Header: Depth=1
	vmovss	(%rdi,%rcx,4), %xmm1            # xmm1 = mem[0],zero,zero,zero
	vmovss	4(%rdi,%rcx,4), %xmm2           # xmm2 = mem[0],zero,zero,zero
	vfmadd132ss	(%rsi,%rcx,4), %xmm0, %xmm1 # xmm1 = (xmm1 * mem) + xmm0
	vfmadd231ss	4(%rsi,%rcx,4), %xmm2, %xmm1 # xmm1 = (xmm2 * mem) + xmm1
	vmovss	8(%rdi,%rcx,4), %xmm2           # xmm2 = mem[0],zero,zero,zero
	vfmadd132ss	8(%rsi,%rcx,4), %xmm1, %xmm2 # xmm2 = (xmm2 * mem) + xmm1
	vmovss	12(%rdi,%rcx,4), %xmm0          # xmm0 = mem[0],zero,zero,zero
	vfmadd132ss	12(%rsi,%rcx,4), %xmm2, %xmm0 # xmm0 = (xmm0 * mem) + xmm2
	addq	$4, %rcx
	cmpl	%ecx, %eax
	jne	.LBB0_8
	jmp	.LBB0_31
.LBB0_12:
	vxorps	%xmm0, %xmm0, %xmm0
	vxorps	%xmm1, %xmm1, %xmm1
	vxorps	%xmm2, %xmm2, %xmm2
	vxorps	%xmm3, %xmm3, %xmm3
	cmpl	$32, %ecx
	jb	.LBB0_15
	.p2align	4, 0x90
.LBB0_20:                               # =>This Inner Loop Header: Depth=1
	vmovups	(%rdi), %ymm4
	vmovups	32(%rdi), %ymm5
	vmovups	64(%rdi), %ymm6
	vmovups	96(%rdi), %ymm7
	vfmadd132ps	(%rsi), %ymm3, %ymm4    # ymm4 = (ymm4 * mem) + ymm3
	vfmadd132ps	32(%rsi), %ymm2, %ymm5  # ymm5 = (ymm5 * mem) + ymm2
	vfmadd132ps	64(%rsi), %ymm1, %ymm6  # ymm6 = (ymm6 * mem) + ymm1
	vfmadd132ps	96(%rsi), %ymm0, %ymm7  # ymm7 = (ymm7 * mem) + ymm0
	vmovups	128(%rdi), %ymm3
	vmovups	160(%rdi), %ymm2
	vmovups	192(%rdi), %ymm1
	vmovups	224(%rdi), %ymm0
	vfmadd132ps	128(%rsi), %ymm4, %ymm3 # ymm3 = (ymm3 * mem) + ymm4
	vfmadd132ps	160(%rsi), %ymm5, %ymm2 # ymm2 = (ymm2 * mem) + ymm5
	vfmadd132ps	192(%rsi), %ymm6, %ymm1 # ymm1 = (ymm1 * mem) + ymm6
	vfmadd132ps	224(%rsi), %ymm7, %ymm0 # ymm0 = (ymm0 * mem) + ymm7
	addl	$-64, %r9d
	addq	$256, %rdi                      # imm = 0x100
	addq	$256, %rsi                      # imm = 0x100
	cmpl	$31, %r9d
	ja	.LBB0_20
# %bb.21:
	movl	%r9d, %ecx
.LBB0_15:
	movl	%ecx, %r9d
	cmpl	$8, %ecx
	jb	.LBB0_18
.LBB0_16:
	movl	%r9d, %ecx
	.p2align	4, 0x90
.LBB0_17:                               # =>This Inner Loop Header: Depth=1
	vmovups	(%rdi), %ymm4
	vfmadd231ps	(%rsi), %ymm4, %ymm3    # ymm3 = (ymm4 * mem) + ymm3
	addl	$-8, %ecx
	addq	$32, %rdi
	addq	$32, %rsi
	cmpl	$7, %ecx
	ja	.LBB0_17
.LBB0_18:
	testl	%ecx, %ecx
	je	.LBB0_19
# %bb.22:
	leal	-1(%rcx), %r8d
	testb	$3, %cl
	je	.LBB0_23
# %bb.24:
	movl	%ecx, %r9d
	andl	$3, %r9d
	vxorps	%xmm4, %xmm4, %xmm4
	xorl	%eax, %eax
	.p2align	4, 0x90
.LBB0_25:                               # =>This Inner Loop Header: Depth=1
	vmovss	(%rdi), %xmm5                   # xmm5 = mem[0],zero,zero,zero
	vfmadd231ss	(%rsi), %xmm5, %xmm4    # xmm4 = (xmm5 * mem) + xmm4
	addq	$4, %rdi
	addq	$4, %rsi
	addq	$1, %rax
	cmpl	%eax, %r9d
	jne	.LBB0_25
# %bb.26:
	subl	%eax, %ecx
	cmpl	$3, %r8d
	jae	.LBB0_28
	jmp	.LBB0_30
.LBB0_19:
	vxorps	%xmm4, %xmm4, %xmm4
	jmp	.LBB0_30
.LBB0_23:
	vxorps	%xmm4, %xmm4, %xmm4
	cmpl	$3, %r8d
	jb	.LBB0_30
.LBB0_28:
	movl	%ecx, %eax
	xorl	%ecx, %ecx
	.p2align	4, 0x90
.LBB0_29:                               # =>This Inner Loop Header: Depth=1
	vmovss	(%rdi,%rcx,4), %xmm5            # xmm5 = mem[0],zero,zero,zero
	vmovss	4(%rdi,%rcx,4), %xmm6           # xmm6 = mem[0],zero,zero,zero
	vfmadd132ss	(%rsi,%rcx,4), %xmm4, %xmm5 # xmm5 = (xmm5 * mem) + xmm4
	vfmadd231ss	4(%rsi,%rcx,4), %xmm6, %xmm5 # xmm5 = (xmm6 * mem) + xmm5
	vmovss	8(%rdi,%rcx,4), %xmm6           # xmm6 = mem[0],zero,zero,zero
	vfmadd132ss	8(%rsi,%rcx,4), %xmm5, %xmm6 # xmm6 = (xmm6 * mem) + xmm5
	vmovss	12(%rdi,%rcx,4), %xmm4          # xmm4 = mem[0],zero,zero,zero
	vfmadd132ss	12(%rsi,%rcx,4), %xmm6, %xmm4 # xmm4 = (xmm4 * mem) + xmm6
	addq	$4, %rcx
	cmpl	%ecx, %eax
	jne	.LBB0_29
.LBB0_30:
	vaddps	%ymm3, %ymm2, %ymm2
	vaddps	%ymm0, %ymm1, %ymm0
	vaddps	%ymm2, %ymm0, %ymm0
	vhaddps	%ymm0, %ymm0, %ymm0
	vhaddps	%ymm0, %ymm0, %ymm0
	vextractf128	$1, %ymm0, %xmm1
	vaddss	%xmm1, %xmm0, %xmm0
	vaddss	%xmm0, %xmm4, %xmm0
	vmovss	%xmm0, (%rdx)
	movq	%rbp, %rsp
	popq	%rbp
	vzeroupper
	retq
.Lfunc_end0:
	.size	dot_256, .Lfunc_end0-dot_256
                                        # -- End function
	.ident	"Ubuntu clang version 14.0.0-1ubuntu1.1"
	.section	".note.GNU-stack","",@progbits
	.addrsig
