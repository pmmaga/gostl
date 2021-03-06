package model

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type Triangle struct {
	Normal        [3]float32
	Vertices      [3][3]float32
	AttrByteCount uint16
}

type Model struct {
	Header       string
	NumTriangles uint32
	Triangles    []Triangle
}

//Stringer method
func (m *Model) String() string {
	mins, maxs := getMinsMaxs(m)
	dimensions := [3]float32{maxs[0] - mins[0], maxs[1] - mins[1], maxs[2] - mins[2]}
	return fmt.Sprintf("Header: %v\nTriangles: %v\nDimensions: %v\nMins: %v\nMaxs: %v\n", m.Header, m.NumTriangles, dimensions, mins, maxs)
}

//ProjectFrom constant to define the Paint perspective
type ProjectFrom int

const (
	ProjectFromSide ProjectFrom = iota
	ProjectFromFront
	ProjectFromTop
)

func (p ProjectFrom) GetAxisForProjection() (forX int, forY int, forValue int) {
	switch p {
	case ProjectFromSide:
		forX = 2
		forY = 1
		forValue = 0
	case ProjectFromFront:
		forX = 2
		forY = 0
		forValue = 1
	case ProjectFromTop:
		forX = 1
		forY = 0
		forValue = 2
	}
	return forX, forY, forValue
}

//Project the model in a matrixSize x matrixSize matrix from the chosen perspective
func ProjectModelVertices(m *Model, matrixSize int, projectFrom ProjectFrom) [][]float32 {
	//Define the perspective
	projectToX, projectToY, projectToValue := projectFrom.GetAxisForProjection()
	//Get the mins and the dimensions
	mins, maxs := getMinsMaxs(m)
	dimensions := [3]float32{maxs[0] - mins[0], maxs[1] - mins[1], maxs[2] - mins[2]}
	//Adjust the scale based on the model dimensions
	scale := float32(1)
	if dimensions[projectToX] > dimensions[projectToY] {
		scale = dimensions[projectToX] / float32(matrixSize)
	} else {
		scale = dimensions[projectToY] / float32(matrixSize)
	}
	//Initialize the output matrix (Y is half the size to compensate for terminal line height)
	matrix := make([][]float32, (matrixSize/2)+1)
	for i := range matrix {
		matrix[i] = make([]float32, matrixSize+1)
	}
	//For each Triangle
	for j := range m.Triangles {
		//For each vertex
		for k := range m.Triangles[j].Vertices {
			//Adjust the coordinates by moving them to the positive space and scaling
			adjustedX, adjustedY := (m.Triangles[j].Vertices[k][projectToX]-mins[projectToX])/scale, (m.Triangles[j].Vertices[k][projectToY]-mins[projectToY])/scale
			matrixX, matrixY := int(adjustedX), int(adjustedY)
			//Mark the vertex in the matrix
			newValue := (m.Triangles[j].Vertices[k][projectToValue] - mins[projectToValue]) / dimensions[projectToValue]
			if newValue > matrix[(matrixSize-matrixX)/2][matrixY] {
				matrix[(matrixSize-matrixX)/2][matrixY] = newValue
			}
		}
	}
	return matrix
}

//Draw a matrix with different characters for the value axis
func DrawMatrix(matrix [][]float32) string {
	//Buffer for the output
	var buffer bytes.Buffer
	for i := range matrix {
		for j := range matrix[i] {
			//Paint each point where a vertex was found
			switch adjVal := matrix[i][j] * 4; {
			case adjVal > 3:
				buffer.WriteString("▓")
			case adjVal > 1.5:
				buffer.WriteString("▒")
			case adjVal > 0:
				buffer.WriteString("░")
			default:
				buffer.WriteString(" ")
			}
		}
		//New row
		buffer.WriteString("\n")
	}
	//fmt.Printf("%v\n", matrix)
	return buffer.String()
}

func CreateFromByteSlice(byteSlice []byte) (m Model, err error) {
	//Read the Header
	m.Header = strings.Trim(string(byteSlice[:80]), "\x00")
	//Read the number of Triangles
	m.NumTriangles = binary.LittleEndian.Uint32(byteSlice[80:84])
	//Read the Triangles
	m.Triangles = make([]Triangle, m.NumTriangles)
	buf := bytes.NewReader(byteSlice[84:])
	err = binary.Read(buf, binary.LittleEndian, &m.Triangles)
	if err != nil {
		return m, err
	}
	return m, nil
}

func CreateFromBinarySTL(r io.Reader) (m Model, err error) {
	//Read the Header and Number of Triangles
	header := make([]byte, 84)
	_, err = io.ReadFull(r, header)
	if err != nil {
		return m, err
	}
	m.Header = strings.Trim(string(header[:80]), "\x00")
	m.NumTriangles = binary.LittleEndian.Uint32(header[80:84])

	//Allocate space for the triangles
	m.Triangles = make([]Triangle, m.NumTriangles)
	err = binary.Read(r, binary.LittleEndian, &m.Triangles)
	if err != nil {
		return m, err
	}
	return m, nil
}

func CreateFromASCIISTL(r *bufio.Reader) (m Model, err error) {
	// Function to treat each line. receives the reader ,the expected starting string, the parts splitters and the number of expected parts after splitting
	readAndTreatLine := func(r *bufio.Reader, mustStartWith string, partSplitters string, expectedPartsLength int) (lineParts []string, err error) {
		//Read a line
		line, err := r.ReadString('\n')
		if err != nil {
			return lineParts, err
		}
		//Trim tabs, spaces and new line
		line = strings.Trim(line, " \t\n\r")
		//Check if size is at least the same as param
		if len(line) < len(mustStartWith) {
			return lineParts, errors.New("Line shorter than mustStartWith.")
		}
		//Check if it starts as expected
		if line[:len(mustStartWith)] != mustStartWith {
			return lineParts, errors.New("Line different from mustStartWith.")
		}
		line = line[len(mustStartWith):]
		lineParts = strings.Split(line, partSplitters)
		if len(lineParts) != expectedPartsLength {
			return lineParts, errors.New("Number of line parts different from expected")
		}
		//Return the line
		return lineParts, nil
	}
	//Read the first line
	Header, err := r.ReadString('\n')
	if err != nil {
		return m, err
	}
	//Create the Header with the original solid name
	m.Header = fmt.Sprintf("Imported from ASCII STL by stl2ascii - %v", strings.Trim(string(Header[5:]), " \n"))
	for {
		var aTriangle Triangle
		//Read the normal
		normalParts, err := readAndTreatLine(r, "facet normal ", " ", 3)
		if err != nil {
			break
		}
		for i := range aTriangle.Normal {
			parsedFloat, err := strconv.ParseFloat(normalParts[i], 32)
			if err != nil {
				return m, err
			}
			aTriangle.Normal[i] = float32(parsedFloat)
		}
		//Read outer loop
		_, err = readAndTreatLine(r, "outer loop", "", 0)
		if err != nil {
			return m, err
		}
		//Read the Vertices
		for j := range aTriangle.Vertices {
			vertexParts, err := readAndTreatLine(r, "vertex ", " ", 3)
			if err != nil {
				return m, err
			}
			for k := range aTriangle.Vertices[j] {
				parsedFloat, err := strconv.ParseFloat(vertexParts[k], 32)
				if err != nil {
					return m, err
				}
				aTriangle.Vertices[j][k] = float32(parsedFloat)
			}
		}
		//Read endloop
		_, err = readAndTreatLine(r, "endloop", "", 0)
		if err != nil {
			return m, err
		}
		//Read endfacet
		_, err = readAndTreatLine(r, "endfacet", "", 0)
		if err != nil {
			return m, err
		}
		m.Triangles = append(m.Triangles, aTriangle)
		m.NumTriangles++
	}

	return m, nil
}

//Get the size for each dimension
func getDimensions(m *Model) [3]float32 {
	mins, maxs := getMinsMaxs(m)
	return [3]float32{maxs[0] - mins[0], maxs[1] - mins[1], maxs[2] - mins[2]}
}

//Get the mins and the maxs arrays
func getMinsMaxs(m *Model) (mins [3]float32, maxs [3]float32) {
	//Initialize arrays for min x y z and max x y z
	mins = [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	maxs = [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	//Run through the Triangles
	for i := range m.Triangles {
		//Each vertice
		for j := range m.Triangles[i].Vertices {
			//Each coordinate
			for k := range m.Triangles[i].Vertices[j] {
				//Update min and max
				if m.Triangles[i].Vertices[j][k] < mins[k] {
					mins[k] = m.Triangles[i].Vertices[j][k]
				}
				if m.Triangles[i].Vertices[j][k] > maxs[k] {
					maxs[k] = m.Triangles[i].Vertices[j][k]
				}
			}
		}
	}
	return mins, maxs
}
