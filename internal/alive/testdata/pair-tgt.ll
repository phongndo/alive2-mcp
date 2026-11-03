declare i8 @g(i8)
define i8 @f(ptr %p) #1 {
  %v = load i8, ptr %p, !range !3
  %w = call i8 @g(i8 %v)
  ret i8 %w
}
define i8 @h(i8 %x) {
  %a = shl i8 %x, 2
  ret i8 %a
}
attributes #1 = { nounwind willreturn }
!3 = !{i8 0, i8 10}
