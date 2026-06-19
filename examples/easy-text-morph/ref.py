from manim import *


class TextMorph(Scene):
    def construct(self):
        # LaTeX reference (manim MathTex). Our pdtt scene renders the same
        # formula NATIVELY via typst. Circle equation x^2 + y^2 -> r^2 -> circle.
        a = MathTex("x^2 + y^2")
        b = MathTex("r^2")
        circle = Circle(radius=1.2, color=WHITE).set_fill(PINK, opacity=0.5)

        a.scale(1.4)
        b.scale(1.4)

        self.play(FadeIn(a))
        self.play(Transform(a, b))
        self.play(Transform(a, circle))
