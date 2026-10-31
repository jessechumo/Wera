package embed

import "math"

// Ridge is a linear model fit by ridge regression. Given a user's AI scores
// and the embeddings of the jobs they scored, it learns which directions in
// embedding space that user's scores follow, so it can estimate a job the AI
// has not seen.
type Ridge struct {
	mean    []float64 // of the training vectors
	weights []float64
	bias    float64 // of the training scores
}

// FitRidge fits scores to vectors, shrinking the weights by lambda (more for
// noisier fits). Vectors of a length other than the first one's are skipped.
// It returns nil when there is nothing to learn from. The cost grows with the
// number of vectors and the square of their length, not with what it later
// predicts: about two seconds for 3,000 scores of 768-long vectors.
func FitRidge(vectors [][]float32, scores []float64, lambda float64) *Ridge {
	if len(vectors) == 0 || len(vectors) != len(scores) || lambda <= 0 {
		return nil
	}
	d := len(vectors[0])
	mean := make([]float64, d)
	var bias float64
	n := 0
	for i, v := range vectors {
		if len(v) != d {
			continue
		}
		for j, x := range v {
			mean[j] += float64(x)
		}
		bias += scores[i] //nolint:gosec // len(vectors) == len(scores), checked above
		n++
	}
	for j := range mean {
		mean[j] /= float64(n)
	}
	bias /= float64(n)

	// The normal equations, lower triangle only: (XᵀX + λI) w = Xᵀy, with X
	// and y centered.
	a := make([]float64, d*d)
	w := make([]float64, d)
	row := make([]float64, d)
	for i, v := range vectors {
		if len(v) != d {
			continue
		}
		for j, x := range v {
			row[j] = float64(x) - mean[j]
		}
		y := scores[i] - bias //nolint:gosec // len(vectors) == len(scores), checked above
		for j, x := range row {
			w[j] += x * y
			for k, z := range row[:j+1] {
				a[j*d+k] += x * z
			}
		}
	}
	for j := range d {
		a[j*d+j] += lambda
	}
	if !solveCholesky(a, w, d) {
		return nil
	}
	return &Ridge{mean: mean, weights: w, bias: bias}
}

// solveCholesky solves A x = b in place, for a symmetric positive definite A
// of size d given by its lower triangle: A becomes its Cholesky factor and b
// the solution. It returns false if A is not positive definite.
func solveCholesky(a, b []float64, d int) bool {
	for j := range d {
		rj := a[j*d : j*d+j+1]
		for k := range j {
			rk := a[k*d : k*d+k+1]
			s := rj[k]
			for m, x := range rk[:k] {
				s -= rj[m] * x
			}
			rj[k] = s / rk[k]
		}
		s := rj[j]
		for _, x := range rj[:j] {
			s -= x * x
		}
		if s <= 0 {
			return false
		}
		rj[j] = math.Sqrt(s)
	}
	for j := range d { // L y = b
		s := b[j]
		for k, x := range a[j*d : j*d+j] {
			s -= x * b[k]
		}
		b[j] = s / a[j*d+j]
	}
	for j := d - 1; j >= 0; j-- { // Lᵀ x = y
		b[j] /= a[j*d+j]
		for k, x := range a[j*d : j*d+j] {
			b[k] -= x * b[j]
		}
	}
	return true
}

// Predict estimates the score of a vector (the mean training score for a
// vector of another length).
func (r *Ridge) Predict(v []float32) float64 {
	if len(v) != len(r.weights) {
		return r.bias
	}
	s := r.bias
	for j, x := range v {
		s += r.weights[j] * (float64(x) - r.mean[j])
	}
	return s
}
